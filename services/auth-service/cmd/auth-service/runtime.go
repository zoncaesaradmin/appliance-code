package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	gen "appliance-code/sdk/golang/gen/messaging"
	"appliance-code/sdk/golang/messaging"
	"appliance-code/sdk/golang/servicehealth"
	_ "github.com/jackc/pgx/v5/stdlib"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var buildVersion = "development"

func loadConfig(get func(string) string) (config, error) {
	c := config{environment: strings.TrimSpace(get("AUTH_SERVICE_ENV")), address: strings.TrimSpace(get("AUTH_SERVICE_LISTEN_ADDR")), databaseURL: strings.TrimSpace(get("AUTH_SERVICE_DATABASE_URL")), origin: strings.TrimSpace(get("AUTH_SERVICE_ORIGIN")), socketParent: strings.TrimSpace(get("AUTH_SERVICE_OTP_SOCKET_PARENT")), deliveryMode: strings.TrimSpace(get("AUTH_SERVICE_OTP_DELIVERY_MODE")), messagingURL: strings.TrimSpace(get("AUTH_SERVICE_MESSAGING_URL")), otpKey: strings.TrimSpace(get("AUTH_SERVICE_OTP_KEY")), rateKey: strings.TrimSpace(get("AUTH_SERVICE_RATE_KEY")), deliveryKey: strings.TrimSpace(get("AUTH_SERVICE_DELIVERY_KEY"))}
	if c.environment == "" {
		c.environment = "local"
	}
	if c.address == "" {
		c.address = ":8080"
	}
	if c.socketParent == "" {
		c.socketParent = "/tmp"
	}
	if c.deliveryMode == "" {
		c.deliveryMode = "local"
	}
	if c.environment != "local" && c.environment != "test" && c.environment != "production" {
		return config{}, errors.New("AUTH_SERVICE_ENV must be local, test, or production")
	}
	if c.deliveryMode != "local" && c.deliveryMode != "event" {
		return config{}, errors.New("AUTH_SERVICE_OTP_DELIVERY_MODE must be local or event")
	}
	if c.environment == "production" && c.deliveryMode == "local" {
		return config{}, errors.New("production auth service requires event OTP delivery")
	}
	if c.databaseURL == "" {
		return config{}, errors.New("AUTH_SERVICE_DATABASE_URL is required")
	}
	u, err := url.Parse(c.origin)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return config{}, errors.New("AUTH_SERVICE_ORIGIN must be an HTTPS origin without a path")
	}
	for _, v := range []struct{ name, value string }{{"AUTH_SERVICE_OTP_KEY", c.otpKey}, {"AUTH_SERVICE_RATE_KEY", c.rateKey}} {
		decoded, e := base64.StdEncoding.DecodeString(v.value)
		if e != nil || len(decoded) < 32 {
			return config{}, fmt.Errorf("%s must be base64-encoded and at least 32 bytes", v.name)
		}
	}
	if c.deliveryMode == "event" {
		if c.messagingURL == "" || c.deliveryKey == "" {
			return config{}, errors.New("AUTH_SERVICE_MESSAGING_URL and AUTH_SERVICE_DELIVERY_KEY are required for event delivery")
		}
		decoded, e := base64.StdEncoding.DecodeString(c.deliveryKey)
		if e != nil || len(decoded) < 32 {
			return config{}, errors.New("AUTH_SERVICE_DELIVERY_KEY must be base64-encoded and at least 32 bytes")
		}
	}
	for _, ch := range strings.Split(strings.TrimSpace(get("AUTH_SERVICE_DELIVERY_CHANNELS")), ",") {
		if ch = strings.TrimSpace(ch); ch != "" {
			c.deliveryChannels = append(c.deliveryChannels, ch)
		}
	}
	if len(c.deliveryChannels) == 0 {
		c.deliveryChannels = []string{"whatsapp"}
	}
	return c, nil
}

type eventSender struct {
	client   *messaging.Client
	key      []byte
	channels []string
}

func (s eventSender) SendOTP(ctx context.Context, r otpRequest) error {
	encrypted, err := messaging.Seal(s.key, []byte(r.Code))
	if err != nil {
		return err
	}
	channels := r.DeliveryChannels
	if len(channels) == 0 {
		channels = s.channels
	}
	return s.client.Publish(ctx, "event.identity.otp.requested", "auth-service", &gen.IdentityEvent{Type: gen.IdentityEvent_OTP_REQUESTED, ChallengeId: r.ChallengeID, Phone: r.Phone, Factor: r.Factor, DeliveryChannels: channels, Purpose: r.Purpose, OriginChannel: r.OriginChannel, Audience: r.Audience, TenantId: r.TenantID, EncryptedCode: encrypted, ExpiresAt: timestamppb.New(r.ExpiresAt)})
}
func connectMessaging(ctx context.Context, url string) (*messaging.Client, error) {
	var client *messaging.Client
	err := servicehealth.RetryPolicy{InitialDelay: time.Second, MaxDelay: 30 * time.Second, AttemptTimeout: 5 * time.Second}.Wait(ctx, func(ctx context.Context) error {
		candidate, err := messaging.Connect(messaging.Options{URL: url, Name: "auth-service", JetStream: true, Timeout: 5 * time.Second})
		if err != nil {
			return err
		}
		if err := candidate.EnsureStream(ctx, messaging.StreamSpec{Name: "identity", Subjects: []string{"event.identity.>"}, MaxAge: 24 * time.Hour}); err != nil {
			candidate.Close()
			return err
		}
		client = candidate
		return nil
	}, func(err error, retryIn time.Duration) {
		fmt.Fprintf(os.Stderr, "{\"level\":\"warn\",\"service\":\"auth-service\",\"event\":\"messaging_unavailable\",\"retry_in\":%q,\"error\":%q}\n", retryIn.String(), err.Error())
	})
	return client, err
}
func waitAuthSchema(ctx context.Context, db *sql.DB) error {
	return servicehealth.RetryPolicy{InitialDelay: time.Second, MaxDelay: 30 * time.Second, AttemptTimeout: 5 * time.Second}.Wait(ctx, func(ctx context.Context) error {
		_, err := db.ExecContext(ctx, authSchemaSQL)
		return err
	}, func(err error, retryIn time.Duration) {
		fmt.Fprintf(os.Stderr, "{\"level\":\"warn\",\"service\":\"auth-service\",\"event\":\"database_unavailable\",\"retry_in\":%q,\"error\":%q}\n", retryIn.String(), err.Error())
	})
}

// initializingHandler keeps the health endpoint available while the service
// waits for its database and messaging dependencies. Kubernetes can then keep
// the pod alive and use readiness, rather than restart loops, to gate traffic.
type initializingHandler struct{ current atomic.Value }

func newInitializingHandler() *initializingHandler {
	h := &initializingHandler{}
	h.current.Store(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health/live":
			w.WriteHeader(http.StatusOK)
		case "/health/ready":
			http.Error(w, "not ready", http.StatusServiceUnavailable)
		default:
			http.Error(w, "not ready", http.StatusServiceUnavailable)
		}
	}))
	return h
}

func (h *initializingHandler) Set(next http.Handler) {
	h.current.Store(http.HandlerFunc(next.ServeHTTP))
}

func (h *initializingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.current.Load().(http.Handler).ServeHTTP(w, r)
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "take-code" {
		if len(os.Args) != 6 || os.Args[2] != "--socket" || os.Args[4] != "--phone" {
			fmt.Fprintln(os.Stderr, "usage: auth-service take-code --socket PATH --phone +...")
			os.Exit(2)
		}
		code, err := takeDevelopmentCode(context.Background(), os.Args[3], os.Args[5])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(code)
		return
	}
	c, err := loadConfig(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	db, err := sql.Open("pgx", c.databaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot open auth database")
		os.Exit(1)
	}
	defer db.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ready := &atomic.Bool{}
	router := newInitializingHandler()
	listener, err := net.Listen("tcp", c.address)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot bind HTTP listener")
		os.Exit(1)
	}
	server := &http.Server{Addr: c.address, Handler: router, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	errCh := make(chan error, 1)
	go func() {
		e := server.Serve(listener)
		if errors.Is(e, http.ErrServerClosed) {
			e = nil
		}
		errCh <- e
	}()
	fmt.Fprintf(os.Stdout, "{\"level\":\"info\",\"service\":\"auth-service\",\"event\":\"service_started\",\"version\":%q,\"address\":%q}\n", buildVersion, c.address)
	if err := waitAuthSchema(ctx, db); err != nil {
		_ = server.Shutdown(context.Background())
		return
	}
	var out sender
	inbox := newLocalInbox()
	var socket *developmentSocket
	if c.deliveryMode == "local" {
		socket, err = newDevelopmentSocket(c.environment, c.socketParent, inbox)
		if err != nil {
			fmt.Fprintln(os.Stderr, "auth OTP socket: ", err)
			os.Exit(1)
		}
		defer socket.close()
		go func() { _ = socket.serve() }()
	}
	var messagingClient *messaging.Client
	if c.deliveryMode == "event" {
		messagingClient, err = connectMessaging(ctx, c.messagingURL)
		if err != nil {
			fmt.Fprintln(os.Stderr, "auth messaging: ", err)
			os.Exit(1)
		}
		defer messagingClient.Close()
		key, _ := base64.StdEncoding.DecodeString(c.deliveryKey)
		out = eventSender{client: messagingClient, key: key, channels: c.deliveryChannels}
	} else {
		out = localSender{inbox: inbox}
	}
	key, _ := base64.StdEncoding.DecodeString(c.otpKey)
	rateKey, _ := base64.StdEncoding.DecodeString(c.rateKey)
	h := authHandler{store: newAuthStore(db, rateKey), sender: out, otpKey: key, origin: c.origin, ready: ready, inbox: inbox, deliveryChannels: append([]string(nil), c.deliveryChannels...)}
	router.Set(h)
	ready.Store(true)
	fmt.Fprintln(os.Stdout, "{\"level\":\"info\",\"service\":\"auth-service\",\"event\":\"dependencies_ready\",\"message\":\"auth dependencies ready\"}")
	select {
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	case err := <-errCh:
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}

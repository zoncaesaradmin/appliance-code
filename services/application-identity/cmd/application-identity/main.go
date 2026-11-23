// application-identity exposes the application user's mobile OTP session API.
// The only delivery adapter currently supported is the explicitly local/test
// development inbox. A production SMS adapter must be added before enabling a
// non-test environment.
package main

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"appliance-code/services/application-identity/internal/identity"
	_ "github.com/jackc/pgx/v5/stdlib"
)

var buildVersion = "development"

const schemaSQL = `
CREATE TABLE IF NOT EXISTS identity_users (id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{32}$'), phone text NOT NULL UNIQUE CHECK (phone ~ '^\+[1-9][0-9]{7,14}$'), created_at timestamptz NOT NULL);
CREATE TABLE IF NOT EXISTS identity_otp_challenges (id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{32}$'), phone text NOT NULL CHECK (phone ~ '^\+[1-9][0-9]{7,14}$'), digest bytea NOT NULL CHECK (octet_length(digest) = 32), expires_at timestamptz NOT NULL, attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0), consumed_at timestamptz);
CREATE INDEX IF NOT EXISTS identity_otp_challenges_expiry_idx ON identity_otp_challenges (expires_at);
CREATE TABLE IF NOT EXISTS identity_otp_issuances (id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, phone_key bytea NOT NULL CHECK (octet_length(phone_key) = 32), client_key bytea NOT NULL CHECK (octet_length(client_key) = 32), created_at timestamptz NOT NULL);
CREATE INDEX IF NOT EXISTS identity_otp_issuances_phone_time_idx ON identity_otp_issuances (phone_key, created_at);
CREATE INDEX IF NOT EXISTS identity_otp_issuances_client_time_idx ON identity_otp_issuances (client_key, created_at);
CREATE TABLE IF NOT EXISTS identity_sessions (token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash) = 32), user_id text NOT NULL REFERENCES identity_users (id), created_at timestamptz NOT NULL, expires_at timestamptz NOT NULL, revoked_at timestamptz, CHECK (expires_at > created_at));
CREATE INDEX IF NOT EXISTS identity_sessions_user_idx ON identity_sessions (user_id);
CREATE INDEX IF NOT EXISTS identity_sessions_expiry_idx ON identity_sessions (expires_at);
CREATE TABLE IF NOT EXISTS identity_request_attempts (id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, bucket_key bytea NOT NULL CHECK (octet_length(bucket_key) = 32), created_at timestamptz NOT NULL);
CREATE INDEX IF NOT EXISTS identity_request_attempts_bucket_time_idx ON identity_request_attempts (bucket_key, created_at);
CREATE INDEX IF NOT EXISTS identity_request_attempts_time_idx ON identity_request_attempts (created_at);`

type config struct {
	environment, address, databaseURL, origin, socketParent string
	otpKey, rateKey                                         []byte
	proxies                                                 []netip.Prefix
}

func loadConfig(get func(string) string) (config, error) {
	c := config{environment: get("APPLICATION_IDENTITY_ENV"), address: get("APPLICATION_IDENTITY_LISTEN_ADDR"), databaseURL: get("APPLICATION_IDENTITY_DATABASE_URL"), origin: get("APPLICATION_IDENTITY_ORIGIN"), socketParent: get("APPLICATION_IDENTITY_OTP_SOCKET_PARENT")}
	if c.environment == "" {
		c.environment = "local"
	}
	if c.environment != "local" && c.environment != "test" {
		return config{}, errors.New("APPLICATION_IDENTITY_ENV must be local or test; production delivery is not available")
	}
	if c.address == "" {
		c.address = ":8080"
	}
	if c.socketParent == "" {
		c.socketParent = "/tmp"
	}
	if c.databaseURL == "" {
		return config{}, errors.New("APPLICATION_IDENTITY_DATABASE_URL is required")
	}
	u, err := url.Parse(c.origin)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return config{}, errors.New("APPLICATION_IDENTITY_ORIGIN must be an HTTPS origin without a path")
	}
	for _, entry := range []struct {
		name string
		dst  *[]byte
	}{{"APPLICATION_IDENTITY_OTP_KEY", &c.otpKey}, {"APPLICATION_IDENTITY_RATE_KEY", &c.rateKey}} {
		decoded, err := base64.StdEncoding.DecodeString(get(entry.name))
		if err != nil || len(decoded) < 32 {
			return config{}, fmt.Errorf("%s must be base64-encoded and at least 32 bytes", entry.name)
		}
		*entry.dst = decoded
	}
	if subtle.ConstantTimeCompare(c.otpKey, c.rateKey) == 1 {
		return config{}, errors.New("OTP and rate keys must be distinct")
	}
	for _, raw := range strings.Split(get("APPLICATION_IDENTITY_TRUSTED_PROXIES"), ",") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil || prefix.Bits() == 0 {
			return config{}, errors.New("APPLICATION_IDENTITY_TRUSTED_PROXIES contains an invalid or universal prefix")
		}
		c.proxies = append(c.proxies, prefix.Masked())
	}
	return c, nil
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "take-code" {
		if len(os.Args) != 6 || os.Args[2] != "--socket" || os.Args[4] != "--phone" {
			fmt.Fprintln(os.Stderr, "usage: application-identity take-code --socket PATH --phone +...")
			os.Exit(2)
		}
		code, err := identity.TakeDevelopmentCode(context.Background(), os.Args[3], os.Args[5])
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
		fmt.Fprintln(os.Stderr, "cannot open identity database")
		os.Exit(1)
	}
	defer db.Close()
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	check, cancel := context.WithTimeout(ctx, 90*time.Second)
	err = waitForSchema(check, db)
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, "identity schema: ", err)
		os.Exit(1)
	}
	inbox, err := identity.NewDevelopmentInbox(c.environment, 5*time.Minute, 1000)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	socket, err := identity.NewDevelopmentSocket(c.environment, inbox, c.socketParent)
	if err != nil {
		fmt.Fprintln(os.Stderr, "identity OTP socket: ", err)
		os.Exit(1)
	}
	defer socket.Close()
	go func() { _ = socket.Serve() }()
	store, err := identity.NewPostgresStore(db, c.rateKey, identity.PostgresLimits{Window: 10 * time.Minute, PerPhone: 5, PerClient: 20, MaxAttempts: 5})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	service, err := identity.New(store, inbox, c.otpKey, 5*time.Minute, 30*24*time.Hour)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	limiter, err := identity.NewPostgresRequestLimiter(db, c.rateKey, 10*time.Minute, map[string]int{"/api/shared/v1/auth/otp/challenges": 20, "/api/shared/v1/auth/otp/verify": 50})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	clientKey, err := identity.TrustedClientKey(c.proxies)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	auth := identity.HTTPHandler{Service: service, Limiter: limiter, ClientKey: clientKey, AllowedOrigin: c.origin}
	mux := http.NewServeMux()
	mux.Handle("/", withHealth(auth, db))
	listener, err := net.Listen("tcp", c.address)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot bind HTTP listener")
		os.Exit(1)
	}
	server := &http.Server{Addr: c.address, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	errCh := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()
	fmt.Fprintf(os.Stdout, "{\"level\":\"info\",\"service\":\"identity\",\"event\":\"service_started\",\"version\":%q,\"address\":%q,\"environment\":%q}\n", buildVersion, c.address, c.environment)
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

func withHealth(auth http.Handler, db *sql.DB) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health/live":
			w.WriteHeader(http.StatusOK)
			return
		case "/health/ready":
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if err := db.PingContext(ctx); err != nil {
				http.Error(w, "not ready", http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		default:
			auth.ServeHTTP(w, r)
		}
	})
}

func ensureSchema(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, schemaSQL)
	return err
}

func waitForSchema(ctx context.Context, db *sql.DB) error {
	var last error
	for {
		if err := ensureSchema(ctx, db); err == nil {
			return nil
		} else {
			last = err
		}
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			if last != nil {
				return last
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

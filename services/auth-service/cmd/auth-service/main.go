// auth-service is the application authentication boundary. The first slice
// exposes the platform capability contract and health endpoints; identity and
// OTP issuance remain owned by application-identity until the auth provider
// integration is promoted into this service.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

var buildVersion = "development"

type config struct {
	address string
}

func loadConfig(get func(string) string) config {
	address := strings.TrimSpace(get("AUTH_SERVICE_LISTEN_ADDR"))
	if address == "" {
		address = ":8080"
	}
	return config{address: address}
}

func handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health/live", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/v1/auth/capabilities", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"service": "auth-service",
			"authentication": map[string]any{
				"methods":          []string{"otp"},
				"deliveryChannels": []string{"whatsapp"},
			},
		})
	})
	return mux
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	c := loadConfig(os.Getenv)
	listener, err := net.Listen("tcp", c.address)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot bind HTTP listener")
		os.Exit(1)
	}
	server := &http.Server{
		Addr:              c.address,
		Handler:           handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	errs := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errs <- err
	}()
	fmt.Fprintf(os.Stdout, "{\"level\":\"info\",\"service\":\"auth-service\",\"event\":\"service_started\",\"version\":%q,\"address\":%q}\n", buildVersion, c.address)
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			os.Exit(1)
		}
	case err := <-errs:
		if err != nil {
			os.Exit(1)
		}
	}
}

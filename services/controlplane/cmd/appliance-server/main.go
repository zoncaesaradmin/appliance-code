// Command appliance-server runs the appliance control plane process: REST
// APIs, the MCP endpoint, and OCI registry token/lifecycle APIs behind one
// shared authn/authz stack. It must build and run as a normal local Go
// binary with no containers or Kubernetes required.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"appliance-code/services/controlplane/internal/app"
	"appliance-code/services/controlplane/internal/config"
	"appliance-code/services/controlplane/internal/logging"
)

func main() {
	if handled, err := dispatchCLI(os.Args[1:]); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, "appliance-server:", err)
			os.Exit(1)
		}
		return
	}

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "appliance-server:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Environ())
	if err != nil {
		return err
	}

	processLogger, err := logging.NewWithWriter(cfg.LogLevel, os.Stdout)
	if err != nil {
		return err
	}
	logFile, err := openApplicationLog(cfg.ApplicationLogPath)
	if err != nil {
		return err
	}
	defer logFile.Close()

	appLogger, err := logging.NewWithWriter(cfg.LogLevel, logFile)
	if err != nil {
		return err
	}
	processLogger.Infow("control plane logger initialized", "applicationLogPath", cfg.ApplicationLogPath)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	bootstrap, err := startBootstrapServer(cfg.InternalAddr)
	if err != nil {
		return err
	}
	defer func() { _ = bootstrap.Close() }()

	type appResult struct {
		application *app.App
		err         error
	}
	resultCh := make(chan appResult, 1)
	go func() {
		application, err := app.New(cfg, appLogger, processLogger)
		resultCh <- appResult{application: application, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil
	case result := <-resultCh:
		if result.err != nil {
			return result.err
		}
		if err := bootstrap.Shutdown(context.Background()); err != nil {
			return fmt.Errorf("stopping bootstrap health server: %w", err)
		}
		return result.application.Run(ctx)
	}
}

func startBootstrapServer(address string) (*http.Server, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("bind bootstrap health listener: %w", err)
	}
	server := &http.Server{
		Addr:              address,
		Handler:           bootstrapHealthHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "bootstrap health server stopped: %v\n", err)
		}
	}()
	return server, nil
}

func bootstrapHealthHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health/live":
			w.WriteHeader(http.StatusOK)
		case "/health/startup":
			http.Error(w, "starting", http.StatusServiceUnavailable)
		case "/health/ready":
			http.Error(w, "not ready", http.StatusServiceUnavailable)
		default:
			http.Error(w, "service initializing", http.StatusServiceUnavailable)
		}
	})
}

func openApplicationLog(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
}

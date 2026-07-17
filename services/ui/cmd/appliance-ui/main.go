package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"appliance-code/services/ui/internal/config"
	"appliance-code/services/ui/internal/controlplane"
	uilogging "appliance-code/services/ui/internal/logging"
	"appliance-code/services/ui/internal/session"
	"appliance-code/services/ui/internal/ui"
)

func main() {
	cfg := config.FromEnv()
	processLogger, err := uilogging.NewWithWriter(cfg.LogLevel.String(), os.Stdout)
	if err != nil {
		_, _ = io.WriteString(os.Stderr, "appliance-ui: initialize process logger: "+err.Error()+"\n")
		os.Exit(1)
	}

	logFile, err := openApplicationLog(cfg.ApplicationLogPath)
	if err != nil {
		_, _ = io.WriteString(os.Stderr, "appliance-ui: open application log: "+err.Error()+"\n")
		os.Exit(1)
	}
	defer logFile.Close()

	appLogger, err := uilogging.NewWithWriter(cfg.LogLevel.String(), logFile)
	if err != nil {
		_, _ = io.WriteString(os.Stderr, "appliance-ui: initialize application logger: "+err.Error()+"\n")
		os.Exit(1)
	}

	cp, err := controlplane.NewClient(controlplane.Config{
		BaseURL:         cfg.ControlPlaneBaseURL,
		InternalBaseURL: cfg.ControlPlaneInternalBaseURL,
		HTTPClient:      &http.Client{Timeout: 10 * time.Second},
		Logger:          appLogger,
		TraceHTTP:       cfg.ControlPlaneTrace,
	})
	if err != nil {
		processLogger.Errorw("initialize control plane client", "error", err)
		os.Exit(1)
	}
	handler, err := ui.New(ui.Config{
		ApplianceProfile: cfg.ApplianceProfile,
		CookieSecure:     cfg.CookieSecure,
		StaticPrefix:     "/static/",
	}, cp, session.NewStore(time.Now), appLogger)
	if err != nil {
		processLogger.Errorw("initialize UI", "error", err)
		os.Exit(1)
	}

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errs := make(chan error, 1)
	go func() {
		processLogger.Infow("starting appliance UI", "addr", cfg.Addr, "applicationLogPath", cfg.ApplicationLogPath)
		errs <- server.ListenAndServe()
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-signals:
		processLogger.Infow("shutting down appliance UI", "signal", sig.String())
	case err := <-errs:
		if !errors.Is(err, http.ErrServerClosed) {
			processLogger.Errorw("appliance UI stopped", "error", err)
			os.Exit(1)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		processLogger.Errorw("appliance UI shutdown failed", "error", err)
		os.Exit(1)
	}
}

func openApplicationLog(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
}

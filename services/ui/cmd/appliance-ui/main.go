package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"appliance-code/services/ui/internal/config"
	"appliance-code/services/ui/internal/controlplane"
	"appliance-code/services/ui/internal/session"
	"appliance-code/services/ui/internal/ui"
)

func main() {
	cfg := config.FromEnv()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))

	cp := controlplane.NewClient(controlplane.Config{
		BaseURL:         cfg.ControlPlaneBaseURL,
		InternalBaseURL: cfg.ControlPlaneInternalBaseURL,
		HTTPClient:      &http.Client{Timeout: 10 * time.Second},
	})
	handler, err := ui.New(ui.Config{
		ApplianceProfile: cfg.ApplianceProfile,
		CookieSecure:     cfg.CookieSecure,
		StaticPrefix:     "/static/",
	}, cp, session.NewStore(time.Now), logger)
	if err != nil {
		logger.Error("initialize UI", "error", err)
		os.Exit(1)
	}

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errs := make(chan error, 1)
	go func() {
		logger.Info("starting appliance UI", "addr", cfg.Addr)
		errs <- server.ListenAndServe()
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-signals:
		logger.Info("shutting down appliance UI", "signal", sig.String())
	case err := <-errs:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("appliance UI stopped", "error", err)
			os.Exit(1)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("appliance UI shutdown failed", "error", err)
		os.Exit(1)
	}
}

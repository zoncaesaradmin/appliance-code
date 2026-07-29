package main

import (
	"context"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"appliance-code/services/hostservice/internal/bridge"
	"appliance-code/services/hostservice/internal/config"
	"appliance-code/services/hostservice/internal/httpapi"
)

func main() {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		log.Fatal(err)
	}

	logger := newLogger(cfg.ApplicationLogPath)
	handler := httpapi.NewHandler(bridge.NewUnixSocketClient(cfg.SocketPath))
	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-shutdownSignal()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()

	logger.Info("starting appliance host agent", "addr", cfg.Addr, "socketPath", cfg.SocketPath)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("host agent failed", "error", err)
		os.Exit(1)
	}
}

func newLogger(logPath string) *slog.Logger {
	writers := []io.Writer{os.Stdout}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err == nil {
		if file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			writers = append(writers, file)
		}
	}
	return slog.New(slog.NewTextHandler(io.MultiWriter(writers...), nil))
}

func shutdownSignal() <-chan os.Signal {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)
	return ch
}

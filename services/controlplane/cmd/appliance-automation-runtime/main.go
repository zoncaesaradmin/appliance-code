package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"appliance-code/services/controlplane/internal/automationruntimeapp"
	"appliance-code/services/controlplane/internal/automationruntimeconfig"
	"appliance-code/services/controlplane/internal/logging"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "appliance-automation-runtime:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := automationruntimeconfig.Load(os.Environ())
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
	application, err := automationruntimeapp.New(cfg, appLogger, processLogger)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return application.Run(ctx)
}

func openApplicationLog(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
}

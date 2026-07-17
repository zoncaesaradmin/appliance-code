// Command appliance-server runs the appliance control plane process: REST
// APIs, the MCP endpoint, and OCI registry token/lifecycle APIs behind one
// shared authn/authz stack. It must build and run as a normal local Go
// binary with no containers or Kubernetes required.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

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

	application, err := app.New(cfg, appLogger, processLogger)
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

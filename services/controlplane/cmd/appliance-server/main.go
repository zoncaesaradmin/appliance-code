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

	logger, err := logging.New(cfg.LogLevel)
	if err != nil {
		return err
	}

	application, err := app.New(cfg, logger)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return application.Run(ctx)
}

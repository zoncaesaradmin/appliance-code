package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"appliance-code/services/controlplane/internal/app"
	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/authn"
	"appliance-code/services/controlplane/internal/bootstrap"
	"appliance-code/services/controlplane/internal/config"
	"appliance-code/services/controlplane/internal/logging"
	"appliance-code/services/controlplane/internal/storage"
)

// readCredentialFile reads a protected password/credential file and trims
// a single trailing newline, so operators can write it with a normal text
// editor without an off-by-one whitespace mismatch.
func readCredentialFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	return strings.TrimRight(string(data), "\r\n"), nil
}

func loadConfigAndServices() (config.Config, *app.Services, error) {
	cfg, err := config.Load(os.Environ())
	if err != nil {
		return config.Config{}, nil, err
	}
	logger, err := logging.NewWithWriter(cfg.LogLevel, os.Stdout)
	if err != nil {
		return config.Config{}, nil, err
	}
	services, err := app.WireServices(cfg, logger)
	if err != nil {
		return config.Config{}, nil, err
	}
	return cfg, services, nil
}

// runBootstrapInit implements `appliance-server bootstrap init`: create the
// first administrator. It succeeds only when no user exists yet and is not
// reachable through any HTTP listener.
func runBootstrapInit(argv []string) error {
	fs := flag.NewFlagSet("bootstrap init", flag.ContinueOnError)
	adminUsername := fs.String("admin-username", "", "initial administrator username (required)")
	passwordFile := fs.String("admin-password-file", "", "path to a file containing the initial administrator password (required)")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *adminUsername == "" || *passwordFile == "" {
		return fmt.Errorf("--admin-username and --admin-password-file are required")
	}

	password, err := readCredentialFile(*passwordFile)
	if err != nil {
		return err
	}

	_, services, err := loadConfigAndServices()
	if err != nil {
		return err
	}
	defer services.DB.Close()

	result, err := bootstrap.Init(context.Background(), services.DB, services.UserStore, services.RoleStore, services.Users, *adminUsername, password, *adminUsername)
	if err != nil {
		return err
	}

	fmt.Printf("bootstrap: created administrator %q (id %s)\n", result.Username, result.AdminUserID)
	return nil
}

// runRecoveryResetPassword implements
// `appliance-server recovery reset-password`: a node-local, root-equivalent
// operator sets a user's password directly, bypassing normal login. It
// revokes every session and API token the user owns.
func runRecoveryResetPassword(argv []string) error {
	fs := flag.NewFlagSet("recovery reset-password", flag.ContinueOnError)
	username := fs.String("username", "", "username to reset (required)")
	passwordFile := fs.String("password-file", "", "path to a file containing the new password (required)")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *username == "" || *passwordFile == "" {
		return fmt.Errorf("--username and --password-file are required")
	}

	newPassword, err := readCredentialFile(*passwordFile)
	if err != nil {
		return err
	}

	normalized, err := authn.NormalizeUsername(*username)
	if err != nil {
		return err
	}

	_, services, err := loadConfigAndServices()
	if err != nil {
		return err
	}
	defer services.DB.Close()

	ctx := context.Background()
	user, err := services.UserStore.GetByUsername(ctx, normalized)
	if err != nil {
		return fmt.Errorf("looking up user %q: %w", normalized, err)
	}

	if err := services.Users.SetPasswordDirect(ctx, audit.Actor{Type: storage.AuditActorSystem, AuthMethod: "break_glass"}, user.ID, newPassword); err != nil {
		return err
	}

	fmt.Printf("recovery: password reset for %q; all sessions and API tokens revoked\n", normalized)
	return nil
}

// dispatchCLI handles the bootstrap and recovery subcommands. It returns
// (handled, err): handled is false when args don't match any subcommand, so
// main falls through to running the server normally.
func dispatchCLI(args []string) (handled bool, err error) {
	if len(args) < 1 {
		return false, nil
	}

	switch args[0] {
	case "bootstrap":
		if len(args) < 2 || args[1] != "init" {
			return true, fmt.Errorf("usage: appliance-server bootstrap init --admin-username <username> --admin-password-file <path>")
		}
		return true, runBootstrapInit(args[2:])
	case "recovery":
		if len(args) < 2 {
			return true, fmt.Errorf("usage: appliance-server recovery <reset-password> ...")
		}
		switch args[1] {
		case "reset-password":
			return true, runRecoveryResetPassword(args[2:])
		default:
			return true, fmt.Errorf("usage: appliance-server recovery <reset-password> ...")
		}
	default:
		return false, nil
	}
}

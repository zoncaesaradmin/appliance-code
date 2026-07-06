// Package app wires configuration, logging, storage, and the public/internal
// HTTP servers together and owns the process's start/run/shutdown lifecycle.
package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"appliance-code/services/controlplane/internal/config"
	"appliance-code/services/controlplane/internal/httpapi"
	"appliance-code/services/controlplane/internal/logging"
	"appliance-code/services/controlplane/internal/mcp"
	"appliance-code/services/controlplane/internal/reqauth"
	"appliance-code/services/controlplane/internal/storage"
)

// App is the wired, runnable control plane process.
type App struct {
	cfg      config.Config
	logger   logging.Logger
	services *Services
	public   *http.Server
	internal *http.Server
	startup  *httpapi.StartupState
}

// readinessAdapter adapts storage.DB to httpapi.ReadinessChecker without
// exposing the rest of the storage surface to the HTTP layer.
type readinessAdapter struct{ db storage.DB }

func (r readinessAdapter) Ready(ctx context.Context) error { return r.db.Ping(ctx) }

// New wires every service and builds the public and internal HTTP servers.
// It does not start listening; call Run for that.
func New(cfg config.Config, logger logging.Logger) (*App, error) {
	services, err := WireServices(cfg)
	if err != nil {
		return nil, err
	}

	startup := &httpapi.StartupState{}
	startup.MarkStarted()

	authDeps := reqauth.Deps{Sessions: services.Sessions, Tokens: services.Tokens, Authz: services.Authz}
	deps := httpapi.Deps{
		Logger:  logger,
		Auth:    authDeps,
		AuthH:   &httpapi.AuthHandlers{Sessions: services.Sessions},
		UsersH:  &httpapi.UserHandlers{Users: services.Users, Roles: services.Roles},
		RolesH:  &httpapi.RoleHandlers{Roles: services.Roles},
		TokensH: &httpapi.TokenHandlers{Tokens: services.Tokens},
		RegistryH: &httpapi.RegistryTokenHandlers{
			Auth: authDeps, Users: services.Users, Authorizer: services.RegistryAuthorizer,
			Keys: services.Keys, Issuer: cfg.CanonicalOrigin,
		},
		RegistryGrantsH: &httpapi.RegistryGrantHandlers{Grants: services.RegistryGrantStore},
		RegistryCatalogH: &httpapi.RegistryCatalogHandlers{
			Zot: services.Zot, Authorizer: services.RegistryAuthorizer, Users: services.Users,
		},
		BuildsH:    &httpapi.BuildHandlers{Builds: services.Builds},
		MCPHandler: mcp.NewHandler(authDeps, cfg.CanonicalOrigin),
	}

	publicHandler := httpapi.NewPublicMux(deps)
	internalHandler := httpapi.NewInternalMux(logger, readinessAdapter{db: services.DB}, startup)

	public := &http.Server{
		Addr:              cfg.PublicAddr,
		Handler:           publicHandler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		MaxHeaderBytes:    int(cfg.MaxHeaderBytes),
	}
	internal := &http.Server{
		Addr:              cfg.InternalAddr,
		Handler:           internalHandler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		MaxHeaderBytes:    int(cfg.MaxHeaderBytes),
	}

	return &App{
		cfg:      cfg,
		logger:   logger,
		services: services,
		public:   public,
		internal: internal,
		startup:  startup,
	}, nil
}

// Run starts both listeners and blocks until ctx is cancelled, then drains
// both servers within the configured shutdown timeout before returning.
func (a *App) Run(ctx context.Context) error {
	errCh := make(chan error, 2)

	go func() {
		a.logger.Infow("public listener starting", "addr", a.cfg.PublicAddr)
		if err := a.public.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("public listener: %w", err)
			return
		}
		errCh <- nil
	}()

	go func() {
		a.logger.Infow("internal listener starting", "addr", a.cfg.InternalAddr)
		if err := a.internal.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("internal listener: %w", err)
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		a.logger.Info("shutdown signal received, draining")
	case err := <-errCh:
		if err != nil {
			a.shutdown()
			return err
		}
	}

	return a.shutdown()
}

func (a *App) shutdown() error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.cfg.ShutdownTimeout)
	defer cancel()

	var errs []error
	if err := a.public.Shutdown(shutdownCtx); err != nil {
		errs = append(errs, fmt.Errorf("shutting down public listener: %w", err))
	}
	if err := a.internal.Shutdown(shutdownCtx); err != nil {
		errs = append(errs, fmt.Errorf("shutting down internal listener: %w", err))
	}
	if err := a.services.DB.Close(); err != nil {
		errs = append(errs, fmt.Errorf("closing storage: %w", err))
	}

	return errors.Join(errs...)
}

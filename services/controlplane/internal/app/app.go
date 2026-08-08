// Package app wires configuration, logging, storage, and the public/internal
// HTTP servers together and owns the process's start/run/shutdown lifecycle.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"appliance-code/services/controlplane/internal/appliance"
	"appliance-code/services/controlplane/internal/config"
	"appliance-code/services/controlplane/internal/httpapi"
	"appliance-code/services/controlplane/internal/logging"
	"appliance-code/services/controlplane/internal/mcp"
	"appliance-code/services/controlplane/internal/reqauth"
	"appliance-code/services/controlplane/internal/storage"
)

// App is the wired, runnable control plane process.
type App struct {
	cfg           config.Config
	logger        logging.Logger
	processLogger logging.Logger
	services      *Services
	public        *http.Server
	internal      *http.Server
	startup       *httpapi.StartupState
}

// readinessAdapter adapts storage.DB to httpapi.ReadinessChecker without
// exposing the rest of the storage surface to the HTTP layer.
type readinessAdapter struct {
	db             storage.DB
	artifactServer interface{ Health(context.Context) error }
	dnsURL         string
	client         *http.Client
}

func (r readinessAdapter) Ready(ctx context.Context) error {
	if err := r.db.Ping(ctx); err != nil {
		return err
	}
	if r.artifactServer != nil {
		if err := r.artifactServer.Health(ctx); err != nil {
			return fmt.Errorf("artifact-server dependency: %w", err)
		}
	}
	if url := strings.TrimSpace(r.dnsURL); url != "" {
		if err := r.probeDNSReady(ctx, url); err != nil {
			return fmt.Errorf("dns dependency: %w", err)
		}
	}
	return nil
}

func (r readinessAdapter) probeDNSReady(ctx context.Context, url string) error {
	client := r.client
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned status %d", url, resp.StatusCode)
	}
	return nil
}

// New wires every service and builds the public and internal HTTP servers.
// It does not start listening; call Run for that.
func New(cfg config.Config, logger, processLogger logging.Logger) (*App, error) {
	if logger == nil {
		return nil, errors.New("application logger is required")
	}
	if processLogger == nil {
		return nil, errors.New("process logger is required")
	}
	resolved, err := cfg.ResolveProfile()
	if err != nil {
		return nil, err
	}
	services, err := wireServices(cfg, resolved, logger)
	if err != nil {
		return nil, err
	}

	startup := &httpapi.StartupState{}
	startup.MarkStarted()

	authDeps := reqauth.Deps{
		Sessions: services.Sessions, Tokens: services.Tokens, Authz: services.Authz,
		Users: services.Users, Roles: services.Roles,
	}
	deps := httpapi.Deps{
		Logger:        logger,
		Auth:          authDeps,
		AuthH:         &httpapi.AuthHandlers{Sessions: services.Sessions, Users: services.Users},
		SetupH:        &httpapi.SetupHandlers{DB: services.DB, UserStore: services.UserStore, RoleStore: services.RoleStore, Users: services.Users},
		CapabilitiesH: &httpapi.CapabilitiesHandlers{Capabilities: services.ApplianceProfile.Capabilities},
		IdentityH: &httpapi.IdentityHandlers{
			ApplianceName:   cfg.ApplianceName,
			DNSZone:         cfg.DNSZoneName,
			NodeIPv4:        cfg.NodeIPv4,
			CanonicalOrigin: cfg.CanonicalOrigin,
		},
		ForwardAuthH: &httpapi.ForwardAuthHandlers{
			Auth: authDeps, Audit: services.Audit, Capabilities: services.ApplianceProfile.Capabilities,
		},
		UsersH:         &httpapi.UserHandlers{Users: services.Users, Roles: services.Roles},
		RolesH:         &httpapi.RoleHandlers{Roles: services.Roles},
		TokensH:        &httpapi.TokenHandlers{Tokens: services.Tokens},
		LANDNSPublishH: &httpapi.LANDNSPublishHandlers{},
		LicensingH:     &httpapi.LicensingHandlers{Licensing: services.Licensing},
		SetupStateH: &httpapi.SetupStateHandlers{
			Licensing: services.Licensing, Profiles: services.Profiles, Metadata: services.Metadata,
			Notifications: services.Notifications, RuntimeProfile: string(services.ApplianceProfile.Name),
		},
		NotificationsH: &httpapi.NotificationHandlers{Notifications: services.Notifications},
		ProfilesH:      &httpapi.ProfileHandlers{Profiles: services.Profiles},
		MetadataH:      &httpapi.MetadataBundleHandlers{Metadata: services.Metadata},
		MCPHandler: mcp.NewHandler(authDeps, cfg.CanonicalOrigin,
			mcp.WithDeveloperWorkflows(services.Devflows, services.ApplianceProfile.Capabilities)),
		ProxiedServices: httpapi.RegistrationsFromRegistry(cfg.ServiceRegistry),
	}
	if appliance.ModuleEnabled(services.Modules, appliance.ModuleNameArtifactRegistry) {
		deps.RegistryH = &httpapi.RegistryTokenHandlers{
			Auth: authDeps, Users: services.Users, Authorizer: services.RegistryAuthorizer,
			Keys: services.Keys, Issuer: cfg.CanonicalOrigin,
		}
		deps.RegistryGrantsH = &httpapi.RegistryGrantHandlers{Grants: services.RegistryGrantStore}
		deps.RegistryCatalogH = &httpapi.RegistryCatalogHandlers{
			ArtifactServer: services.ArtifactServer, Authorizer: services.RegistryAuthorizer, Users: services.Users,
		}
	}
	if appliance.ModuleEnabled(services.Modules, appliance.ModuleNameFiles) {
		deps.FilesH = &httpapi.FileHandlers{
			RootDir:         cfg.FilesRootDir,
			MaxUploadBytes:  cfg.FilesMaxUploadBytes,
			TransferTimeout: cfg.FilesTransferTimeout,
		}
	}
	if appliance.ModuleEnabled(services.Modules, appliance.ModuleNameBuild) {
		deps.BuildsH = &httpapi.BuildHandlers{Builds: services.Builds}
		deps.DevflowsH = &httpapi.DeveloperWorkflowHandlers{Devflows: services.Devflows, BuilderGit: services.BuilderGit, Logger: logger}
	}
	if appliance.ModuleEnabled(services.Modules, appliance.ModuleNameLANDNS) {
		if services.DNS == nil {
			services.DB.Close()
			return nil, fmt.Errorf("building public mux: dns capability enabled but DNS service is nil")
		}
		deps.DNSH = &httpapi.DNSHandlers{DNS: services.DNS}
	}

	publicHandler, err := httpapi.NewPublicMux(deps, services.ApplianceProfile.Capabilities, services.Modules)
	if err != nil {
		services.DB.Close()
		return nil, fmt.Errorf("building public mux: %w", err)
	}
	internalHandler := httpapi.NewInternalMux(logger, readinessAdapter{
		db: services.DB, artifactServer: services.ArtifactServer, dnsURL: cfg.DNSReadyURL,
		client: &http.Client{Timeout: 3 * time.Second},
	}, startup)

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
		cfg:           cfg,
		logger:        logger,
		processLogger: processLogger,
		services:      services,
		public:        public,
		internal:      internal,
		startup:       startup,
	}, nil
}

// Run starts both listeners and blocks until ctx is cancelled, then drains
// both servers within the configured shutdown timeout before returning.
func (a *App) Run(ctx context.Context) error {
	errCh := make(chan error, 2)

	go func() {
		a.processLogger.Infow("public listener starting", "addr", a.cfg.PublicAddr)
		if err := a.public.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("public listener: %w", err)
			return
		}
		errCh <- nil
	}()

	go func() {
		a.processLogger.Infow("internal listener starting", "addr", a.cfg.InternalAddr)
		if err := a.internal.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("internal listener: %w", err)
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		a.processLogger.Info("shutdown signal received, draining")
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

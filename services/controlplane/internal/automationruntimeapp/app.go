package automationruntimeapp

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/auditops"
	"appliance-code/services/controlplane/internal/automationruntimeapi"
	"appliance-code/services/controlplane/internal/automationruntimeauth"
	"appliance-code/services/controlplane/internal/automationruntimeconfig"
	"appliance-code/services/controlplane/internal/keys"
	"appliance-code/services/controlplane/internal/logging"
	"appliance-code/services/controlplane/internal/metadatabundle"
	"appliance-code/services/controlplane/internal/storage"
	"appliance-code/services/controlplane/internal/storage/sqlite"
)

type App struct {
	cfg           automationruntimeconfig.Config
	logger        logging.Logger
	processLogger logging.Logger
	db            storage.DB
	server        *http.Server
}

func New(cfg automationruntimeconfig.Config, logger, processLogger logging.Logger) (*App, error) {
	if logger == nil {
		return nil, errors.New("application logger is required")
	}
	if processLogger == nil {
		return nil, errors.New("process logger is required")
	}
	db, err := sqlite.Open(automationruntimeconfig.SQLitePath(cfg.DataDir))
	if err != nil {
		return nil, fmt.Errorf("automation runtime: opening storage: %w", err)
	}
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("automation runtime: migrating storage: %w", err)
	}
	keyMaterial, err := keys.LoadOrGenerate(cfg.DataDir + "/keys")
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("automation runtime: loading key material: %w", err)
	}
	auditStore := sqlite.NewAuditStore(db)
	operationsStore := sqlite.NewOperationsStore(db)
	auditOps, err := auditops.NewService(auditStore, operationsStore, cfg.DataDir, cfg.AuditRetentionDays, logger)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("automation runtime: initializing audit ops: %w", err)
	}
	recorder := audit.NewRecorder(auditStore)
	metadataSvc, err := metadatabundle.NewService(db, sqlite.NewMetadataBundleStore(db), recorder, auditOps, cfg.DataDir)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("automation runtime: initializing metadata bundle: %w", err)
	}
	handler := automationruntimeapi.NewMux(automationruntimeapi.Deps{
		Logger:        logger,
		Metadata:      metadataSvc,
		InternalToken: automationruntimeauth.TokenFromPepper(keyMaterial.APITokenPepper),
	})
	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		MaxHeaderBytes:    int(cfg.MaxHeaderBytes),
	}
	return &App{cfg: cfg, logger: logger, processLogger: processLogger, db: db, server: server}, nil
}

func (a *App) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		a.processLogger.Infow("automation runtime listener starting", "addr", a.cfg.Addr)
		if err := a.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), a.cfg.ShutdownTimeout)
		defer cancel()
		err := a.server.Shutdown(shutdownCtx)
		_ = a.db.Close()
		return err
	case err := <-errCh:
		_ = a.db.Close()
		return err
	}
}

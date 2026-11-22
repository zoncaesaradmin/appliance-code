package actionruntimeapp

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"appliance-code/services/controlplane/internal/actionruntimeapi"
	"appliance-code/services/controlplane/internal/actionruntimeauth"
	"appliance-code/services/controlplane/internal/actionruntimeconfig"
	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/auditops"
	"appliance-code/services/controlplane/internal/keys"
	"appliance-code/services/controlplane/internal/logging"
	"appliance-code/services/controlplane/internal/metadatabundle"
	"appliance-code/services/controlplane/internal/storage"
	"appliance-code/services/controlplane/internal/storage/sqlstore"
)

type App struct {
	cfg           actionruntimeconfig.Config
	logger        logging.Logger
	processLogger logging.Logger
	db            storage.DB
	server        *http.Server
}

func New(cfg actionruntimeconfig.Config, logger, processLogger logging.Logger) (*App, error) {
	if logger == nil {
		return nil, errors.New("application logger is required")
	}
	if processLogger == nil {
		return nil, errors.New("process logger is required")
	}
	db, err := sqlstore.OpenSQLite(actionruntimeconfig.SQLitePath(cfg.DataDir))
	if err != nil {
		return nil, fmt.Errorf("action runtime: opening storage: %w", err)
	}
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("action runtime: migrating storage: %w", err)
	}
	apiTokenPepper, err := keys.LoadAPITokenPepper(cfg.DataDir + "/keys")
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("action runtime: loading key material: %w", err)
	}
	auditStore := sqlstore.NewAuditStore(db)
	operationsStore := sqlstore.NewOperationsStore(db)
	auditOps, err := auditops.NewService(auditStore, operationsStore, cfg.DataDir, cfg.AuditRetentionDays, logger)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("action runtime: initializing audit ops: %w", err)
	}
	recorder := audit.NewRecorder(auditStore)
	metadataSvc, err := metadatabundle.NewService(db, sqlstore.NewMetadataBundleStore(db), recorder, auditOps, cfg.DataDir)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("action runtime: initializing metadata bundle: %w", err)
	}
	handler := actionruntimeapi.NewMux(actionruntimeapi.Deps{
		Logger:        logger,
		Metadata:      metadataSvc,
		InternalToken: actionruntimeauth.TokenFromPepper(apiTokenPepper),
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
		a.processLogger.Infow("action runtime listener starting", "addr", a.cfg.Addr)
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

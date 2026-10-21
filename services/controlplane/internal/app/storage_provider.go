package app

import (
	"context"
	"fmt"
	"strings"

	"appliance-code/services/controlplane/internal/config"
	"appliance-code/services/controlplane/internal/storage"
	"appliance-code/services/controlplane/internal/storage/sqlstore"
)

// StorageSet is the complete set of persistence contracts needed by the
// control plane. A provider must create these stores from one database so
// transaction boundaries remain meaningful across domain services.
//
// Keeping this construction boundary here prevents application wiring from
// depending on a concrete SQL backend. The PostgreSQL provider supplies this
// complete set for appliance and cloud deployments.
type StorageSet struct {
	DB                  storage.DB
	UserStore           storage.UserStore
	RoleStore           storage.RoleStore
	TokenStore          storage.TokenStore
	SessionStore        storage.SessionStore
	ThrottleStore       storage.ThrottleStore
	AuditStore          storage.AuditStore
	OperationsStore     storage.OperationsStore
	RegistryGrantStore  storage.RegistryGrantStore
	DNSRecordStore      storage.DNSRecordStore
	LicensingStore      storage.LicensingStore
	MetadataStore       storage.MetadataBundleStore
	BuildStore          storage.BuildStore
	IdempotencyStore    storage.IdempotencyStore
	WorkspaceStore      storage.WorkspaceStore
	JobStore            storage.JobStore
	ApplicationStore    storage.ApplicationStore
	FocusContentStore   storage.FocusContentStore
	WebUIGrantStore     storage.WebUILaunchGrantStore
	WebUIBridgeStore    storage.WebUIBridgeSessionStore
	BuilderCatalogStore storage.BuilderCatalogStore
	ConfigurationStore  storage.ConfigurationRevisionStore
}

// StorageProvider opens and migrates one complete persistence backend. The
// returned set is intentionally all-or-nothing: mixing stores from different
// databases would invalidate storage.DB transaction guarantees.
type StorageProvider interface {
	Open(context.Context, config.Config) (StorageSet, error)
}

// storageProviderFor is the sole backend-selection point. Appliance-local
// PostgreSQL and a cloud-managed PostgreSQL instance both select "postgres";
// only the secret-supplied DSN differs. Unsupported drivers never fall back
// to another backend.
func storageProviderFor(cfg config.Config) (StorageProvider, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Database.Driver)) {
	case "sqlite":
		return localSQLStorageProvider{}, nil
	case "postgres":
		return postgresStorageProvider{}, nil
	default:
		return nil, fmt.Errorf("unsupported database driver %q", cfg.Database.Driver)
	}
}

type localSQLStorageProvider struct{}

func (localSQLStorageProvider) Open(ctx context.Context, cfg config.Config) (StorageSet, error) {
	db, err := sqlstore.OpenSQLite(cfg.SQLitePath())
	if err != nil {
		return StorageSet{}, fmt.Errorf("opening SQLite storage: %w", err)
	}
	if err := db.Migrate(ctx); err != nil {
		_ = db.Close()
		return StorageSet{}, fmt.Errorf("migrating SQLite storage: %w", err)
	}
	return StorageSet{
		DB:                  db,
		UserStore:           sqlstore.NewUserStore(db),
		RoleStore:           sqlstore.NewRoleStore(db),
		TokenStore:          sqlstore.NewTokenStore(db),
		SessionStore:        sqlstore.NewSessionStore(db),
		ThrottleStore:       sqlstore.NewThrottleStore(db),
		AuditStore:          sqlstore.NewAuditStore(db),
		OperationsStore:     sqlstore.NewOperationsStore(db),
		RegistryGrantStore:  sqlstore.NewRegistryGrantStore(db),
		DNSRecordStore:      sqlstore.NewDNSRecordStore(db),
		LicensingStore:      sqlstore.NewLicensingStore(db),
		MetadataStore:       sqlstore.NewMetadataBundleStore(db),
		BuildStore:          sqlstore.NewBuildStore(db),
		IdempotencyStore:    sqlstore.NewIdempotencyStore(db),
		WorkspaceStore:      sqlstore.NewWorkspaceStore(db),
		JobStore:            sqlstore.NewJobStore(db),
		ApplicationStore:    sqlstore.NewApplicationStore(db),
		FocusContentStore:   sqlstore.NewFocusContentStore(db),
		WebUIGrantStore:     sqlstore.NewWebUILaunchGrantStore(db),
		WebUIBridgeStore:    sqlstore.NewWebUIBridgeSessionStore(db),
		BuilderCatalogStore: sqlstore.NewBuilderCatalogStore(db),
		ConfigurationStore:  sqlstore.NewConfigurationRevisionStore(db),
	}, nil
}

type postgresStorageProvider struct{}

func (postgresStorageProvider) Open(ctx context.Context, cfg config.Config) (StorageSet, error) {
	db, err := sqlstore.OpenPostgres(ctx, cfg.Database.DSN, cfg.Database.MaxOpenConns, cfg.Database.MaxIdleConns, cfg.Database.ConnMaxLifetime)
	if err != nil {
		return StorageSet{}, fmt.Errorf("opening PostgreSQL storage: %w", err)
	}
	if err := db.Migrate(ctx); err != nil {
		_ = db.Close()
		return StorageSet{}, fmt.Errorf("migrating PostgreSQL storage: %w", err)
	}
	return StorageSet{
		DB:                  db,
		UserStore:           sqlstore.NewUserStore(db),
		RoleStore:           sqlstore.NewRoleStore(db),
		TokenStore:          sqlstore.NewTokenStore(db),
		SessionStore:        sqlstore.NewSessionStore(db),
		ThrottleStore:       sqlstore.NewThrottleStore(db),
		AuditStore:          sqlstore.NewAuditStore(db),
		OperationsStore:     sqlstore.NewOperationsStore(db),
		RegistryGrantStore:  sqlstore.NewRegistryGrantStore(db),
		DNSRecordStore:      sqlstore.NewDNSRecordStore(db),
		LicensingStore:      sqlstore.NewLicensingStore(db),
		MetadataStore:       sqlstore.NewMetadataBundleStore(db),
		BuildStore:          sqlstore.NewBuildStore(db),
		IdempotencyStore:    sqlstore.NewIdempotencyStore(db),
		WorkspaceStore:      sqlstore.NewWorkspaceStore(db),
		JobStore:            sqlstore.NewJobStore(db),
		ApplicationStore:    sqlstore.NewApplicationStore(db),
		FocusContentStore:   sqlstore.NewFocusContentStore(db),
		WebUIGrantStore:     sqlstore.NewWebUILaunchGrantStore(db),
		WebUIBridgeStore:    sqlstore.NewWebUIBridgeSessionStore(db),
		BuilderCatalogStore: sqlstore.NewBuilderCatalogStore(db),
		ConfigurationStore:  sqlstore.NewConfigurationRevisionStore(db),
	}, nil
}

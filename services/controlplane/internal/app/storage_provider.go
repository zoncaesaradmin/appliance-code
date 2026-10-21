package app

import (
	"context"
	"fmt"
	"strings"

	"appliance-code/services/controlplane/internal/config"
	"appliance-code/services/controlplane/internal/storage"
	"appliance-code/services/controlplane/internal/storage/sqlite"
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
		return sqliteStorageProvider{}, nil
	case "postgres":
		return postgresStorageProvider{}, nil
	default:
		return nil, fmt.Errorf("unsupported database driver %q", cfg.Database.Driver)
	}
}

type sqliteStorageProvider struct{}

func (sqliteStorageProvider) Open(ctx context.Context, cfg config.Config) (StorageSet, error) {
	db, err := sqlite.Open(cfg.SQLitePath())
	if err != nil {
		return StorageSet{}, fmt.Errorf("opening SQLite storage: %w", err)
	}
	if err := db.Migrate(ctx); err != nil {
		_ = db.Close()
		return StorageSet{}, fmt.Errorf("migrating SQLite storage: %w", err)
	}
	return StorageSet{
		DB:                  db,
		UserStore:           sqlite.NewUserStore(db),
		RoleStore:           sqlite.NewRoleStore(db),
		TokenStore:          sqlite.NewTokenStore(db),
		SessionStore:        sqlite.NewSessionStore(db),
		ThrottleStore:       sqlite.NewThrottleStore(db),
		AuditStore:          sqlite.NewAuditStore(db),
		OperationsStore:     sqlite.NewOperationsStore(db),
		RegistryGrantStore:  sqlite.NewRegistryGrantStore(db),
		DNSRecordStore:      sqlite.NewDNSRecordStore(db),
		LicensingStore:      sqlite.NewLicensingStore(db),
		MetadataStore:       sqlite.NewMetadataBundleStore(db),
		BuildStore:          sqlite.NewBuildStore(db),
		IdempotencyStore:    sqlite.NewIdempotencyStore(db),
		WorkspaceStore:      sqlite.NewWorkspaceStore(db),
		JobStore:            sqlite.NewJobStore(db),
		ApplicationStore:    sqlite.NewApplicationStore(db),
		FocusContentStore:   sqlite.NewFocusContentStore(db),
		WebUIGrantStore:     sqlite.NewWebUILaunchGrantStore(db),
		WebUIBridgeStore:    sqlite.NewWebUIBridgeSessionStore(db),
		BuilderCatalogStore: sqlite.NewBuilderCatalogStore(db),
	}, nil
}

type postgresStorageProvider struct{}

func (postgresStorageProvider) Open(ctx context.Context, cfg config.Config) (StorageSet, error) {
	db, err := sqlite.OpenPostgres(ctx, cfg.Database.DSN, cfg.Database.MaxOpenConns, cfg.Database.MaxIdleConns, cfg.Database.ConnMaxLifetime)
	if err != nil {
		return StorageSet{}, fmt.Errorf("opening PostgreSQL storage: %w", err)
	}
	if err := db.Migrate(ctx); err != nil {
		_ = db.Close()
		return StorageSet{}, fmt.Errorf("migrating PostgreSQL storage: %w", err)
	}
	return StorageSet{
		DB:                  db,
		UserStore:           sqlite.NewUserStore(db),
		RoleStore:           sqlite.NewRoleStore(db),
		TokenStore:          sqlite.NewTokenStore(db),
		SessionStore:        sqlite.NewSessionStore(db),
		ThrottleStore:       sqlite.NewThrottleStore(db),
		AuditStore:          sqlite.NewAuditStore(db),
		OperationsStore:     sqlite.NewOperationsStore(db),
		RegistryGrantStore:  sqlite.NewRegistryGrantStore(db),
		DNSRecordStore:      sqlite.NewDNSRecordStore(db),
		LicensingStore:      sqlite.NewLicensingStore(db),
		MetadataStore:       sqlite.NewMetadataBundleStore(db),
		BuildStore:          sqlite.NewBuildStore(db),
		IdempotencyStore:    sqlite.NewIdempotencyStore(db),
		WorkspaceStore:      sqlite.NewWorkspaceStore(db),
		JobStore:            sqlite.NewJobStore(db),
		ApplicationStore:    sqlite.NewApplicationStore(db),
		FocusContentStore:   sqlite.NewFocusContentStore(db),
		WebUIGrantStore:     sqlite.NewWebUILaunchGrantStore(db),
		WebUIBridgeStore:    sqlite.NewWebUIBridgeSessionStore(db),
		BuilderCatalogStore: sqlite.NewBuilderCatalogStore(db),
	}, nil
}

package app

import (
	"context"
	"fmt"

	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/authn"
	"appliance-code/services/controlplane/internal/authz"
	"appliance-code/services/controlplane/internal/builds"
	"appliance-code/services/controlplane/internal/config"
	"appliance-code/services/controlplane/internal/keys"
	"appliance-code/services/controlplane/internal/registryauth"
	"appliance-code/services/controlplane/internal/roles"
	"appliance-code/services/controlplane/internal/storage"
	"appliance-code/services/controlplane/internal/storage/sqlite"
	"appliance-code/services/controlplane/internal/tokens"
	"appliance-code/services/controlplane/internal/users"
	"appliance-code/services/controlplane/internal/workflows"
	"appliance-code/services/controlplane/internal/zotadapter"
)

// SessionAudience identifies the API audience session JWTs are issued for.
const SessionAudience = "appliance-api"

// Services holds every business-logic dependency shared by the HTTP server
// and the CLI bootstrap/recovery commands, so both wire identically instead
// of duplicating storage/service construction.
type Services struct {
	DB storage.DB

	UserStore          storage.UserStore
	RoleStore          storage.RoleStore
	AuditStore         storage.AuditStore
	RegistryGrantStore storage.RegistryGrantStore
	BuildStore         storage.BuildStore
	IdempotencyStore   storage.IdempotencyStore

	Users              *users.Service
	Roles              *roles.Service
	Tokens             *tokens.Service
	Sessions           *authn.SessionService
	Authz              *authz.Service
	RegistryAuthorizer *registryauth.Authorizer
	Zot                zotadapter.Client
	WorkflowEngine     workflows.Engine
	Builds             *builds.Service

	Keys  *keys.Material
	Audit *audit.Recorder
}

// WireServices opens storage, migrates it, seeds the built-in role/
// permission catalog, loads key material, and constructs every business
// service. It does not start any HTTP listener, so it is also used directly
// by the bootstrap and recovery CLI commands.
func WireServices(cfg config.Config) (*Services, error) {
	db, err := sqlite.Open(cfg.SQLitePath())
	if err != nil {
		return nil, fmt.Errorf("app: opening storage: %w", err)
	}

	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("app: migrating storage: %w", err)
	}

	roleStore := sqlite.NewRoleStore(db)
	if err := roles.Seed(ctx, roleStore); err != nil {
		db.Close()
		return nil, fmt.Errorf("app: seeding roles: %w", err)
	}

	keyMaterial, err := keys.LoadOrGenerate(cfg.KeysDir())
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("app: loading key material: %w", err)
	}

	userStore := sqlite.NewUserStore(db)
	tokenStore := sqlite.NewTokenStore(db)
	sessionStore := sqlite.NewSessionStore(db)
	throttleStore := sqlite.NewThrottleStore(db)
	auditStore := sqlite.NewAuditStore(db)
	registryGrantStore := sqlite.NewRegistryGrantStore(db)
	recorder := audit.NewRecorder(auditStore)

	var zotClient zotadapter.Client
	if cfg.ZotBaseURL != "" {
		zotClient = zotadapter.NewHTTPClient(cfg.ZotBaseURL, nil, nil)
	} else {
		zotClient = zotadapter.NewFake()
	}

	buildStore := sqlite.NewBuildStore(db)
	idempotencyStore := sqlite.NewIdempotencyStore(db)
	// The real internal/workflows/argo adapter is not implemented in this
	// pass (see internal/workflows's doc comment): validating it needs a
	// real K3s host with the namespace-scoped Argo Workflow Controller and
	// the rootless Buildah isolation gate proven, neither available here.
	workflowEngine := workflows.NewFake()
	buildsSvc := builds.NewService(db, buildStore, idempotencyStore, workflowEngine, recorder,
		cfg.AllowedGitSourceHosts, cfg.AllowedBuilderImageDigests, cfg.BuildDefaultDeadline)

	return &Services{
		DB: db, UserStore: userStore, RoleStore: roleStore, AuditStore: auditStore, RegistryGrantStore: registryGrantStore,
		BuildStore: buildStore, IdempotencyStore: idempotencyStore,
		Users:              users.NewService(db, userStore, roleStore, tokenStore, sessionStore, throttleStore, recorder, keyMaterial),
		Roles:              roles.NewService(db, roleStore, userStore, recorder),
		Tokens:             tokens.NewService(db, tokenStore, recorder, keyMaterial),
		Sessions:           authn.NewSessionService(db, userStore, sessionStore, throttleStore, recorder, keyMaterial, cfg.CanonicalOrigin, SessionAudience),
		Authz:              authz.NewService(roleStore),
		RegistryAuthorizer: registryauth.NewAuthorizer(registryGrantStore, roleStore),
		Zot:                zotClient,
		WorkflowEngine:     workflowEngine,
		Builds:             buildsSvc,
		Keys:               keyMaterial,
		Audit:              recorder,
	}, nil
}

package app

import (
	"context"
	"fmt"

	"appliance-code/services/controlplane/internal/appliance"
	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/authn"
	"appliance-code/services/controlplane/internal/authz"
	"appliance-code/services/controlplane/internal/buildergit"
	"appliance-code/services/controlplane/internal/builds"
	"appliance-code/services/controlplane/internal/config"
	"appliance-code/services/controlplane/internal/devflows"
	"appliance-code/services/controlplane/internal/keys"
	"appliance-code/services/controlplane/internal/logging"
	"appliance-code/services/controlplane/internal/registryauth"
	"appliance-code/services/controlplane/internal/roles"
	"appliance-code/services/controlplane/internal/storage"
	"appliance-code/services/controlplane/internal/storage/sqlite"
	"appliance-code/services/controlplane/internal/tokens"
	"appliance-code/services/controlplane/internal/users"
	"appliance-code/services/controlplane/internal/workflows"
	"appliance-code/services/controlplane/internal/workflows/argo"
	"appliance-code/services/controlplane/internal/zotadapter"
)

const (
	SessionAudience       = "appliance-api"
	argoWorkflowNamespace = "appliance-builds"
)

type Services struct {
	ApplianceProfile appliance.ResolvedProfile

	DB storage.DB

	UserStore          storage.UserStore
	RoleStore          storage.RoleStore
	AuditStore         storage.AuditStore
	RegistryGrantStore storage.RegistryGrantStore
	BuildStore         storage.BuildStore
	IdempotencyStore   storage.IdempotencyStore
	WorkspaceStore     storage.WorkspaceStore
	JobStore           storage.JobStore

	Users              *users.Service
	Roles              *roles.Service
	Tokens             *tokens.Service
	Sessions           *authn.SessionService
	Authz              *authz.Service
	RegistryAuthorizer *registryauth.Authorizer
	Zot                zotadapter.Client
	WorkflowEngine     workflows.Engine
	Builds             *builds.Service
	Devflows           *devflows.Service
	BuilderGit         *buildergit.Service

	Keys  *keys.Material
	Audit *audit.Recorder
}

func WireServices(cfg config.Config, logger logging.Logger) (*Services, error) {
	resolved, err := appliance.ResolveProfile(cfg.ApplianceProfile)
	if err != nil {
		return nil, fmt.Errorf("app: resolving appliance profile: %w", err)
	}
	return wireServices(cfg, resolved, logger)
}

func wireServices(cfg config.Config, resolved appliance.ResolvedProfile, logger logging.Logger) (*Services, error) {
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
	var registryAuthorizer *registryauth.Authorizer
	if resolved.Capabilities.Enabled(appliance.CapabilityArtifact) {
		registryAuthorizer = registryauth.NewAuthorizer(registryGrantStore, roleStore)
		if cfg.ZotBaseURL != "" {
			zotClient = zotadapter.NewHTTPClient(cfg.ZotBaseURL, nil, nil)
		} else {
			zotClient = zotadapter.NewFake()
		}
	}

	buildStore := sqlite.NewBuildStore(db)
	idempotencyStore := sqlite.NewIdempotencyStore(db)
	workspaceStore := sqlite.NewWorkspaceStore(db)
	jobStore := sqlite.NewJobStore(db)
	var workflowEngine workflows.Engine
	if resolved.Capabilities.Enabled(appliance.CapabilityBuild) {
		switch cfg.WorkflowEngine {
		case "fake":
			workflowEngine = workflows.NewFake()
		case "argo":
			var err error
			workflowEngine, err = argo.NewInCluster(argoWorkflowNamespace, cfg.WorkflowInstanceID, cfg.WorkflowExecutorServiceAccount)
			if err != nil {
				db.Close()
				return nil, fmt.Errorf("app: wiring argo workflow engine: %w", err)
			}
		}
	}

	var buildsSvc *builds.Service
	var devflowsSvc *devflows.Service
	var builderGitSvc *buildergit.Service
	if resolved.Capabilities.Enabled(appliance.CapabilityBuild) {
		allowedGitHosts, err := cfg.BuildCatalog.RepoHosts()
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("app: deriving build catalog git hosts: %w", err)
		}
		secretManager := buildergit.SecretManager(buildergit.NewMemorySecretManager())
		if cfg.WorkflowEngine == "argo" {
			secretManager, err = buildergit.NewInClusterSecretManager()
			if err != nil {
				db.Close()
				return nil, fmt.Errorf("app: wiring builder Git secret manager: %w", err)
			}
		}
		builderGitSvc, err = buildergit.NewService(secretManager, argoWorkflowNamespace, buildergit.DefaultSecretName, allowedGitHosts)
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("app: wiring builder Git service: %w", err)
		}
		buildsSvc = builds.NewService(db, buildStore, idempotencyStore, workflowEngine, recorder,
			allowedGitHosts, cfg.BuildCatalog.BuilderImageDigests(), cfg.BuildDefaultDeadline,
			cfg.WorkspaceRootDir, cfg.WorkspaceClaimName, builderGitSvc, cfg.BuildCatalog.SensitiveLogValues()...)
		devflowsSvc, err = devflows.NewService(cfg.BuildCatalog, workspaceStore, jobStore, buildsSvc, workflowEngine, cfg.WorkspaceProvisionerImageDigest, cfg.WorkspaceRootDir, cfg.WorkspaceClaimName, builderGitSvc, logger, recorder)
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("app: wiring developer workflows: %w", err)
		}
		if err := buildsSvc.ReconcileAll(ctx); err != nil {
			db.Close()
			return nil, fmt.Errorf("app: reconciling builds: %w", err)
		}
		if err := devflowsSvc.ReconcileAll(ctx); err != nil {
			db.Close()
			return nil, fmt.Errorf("app: reconciling developer workflow jobs: %w", err)
		}
	}

	return &Services{
		ApplianceProfile:   resolved,
		DB:                 db,
		UserStore:          userStore,
		RoleStore:          roleStore,
		AuditStore:         auditStore,
		RegistryGrantStore: registryGrantStore,
		BuildStore:         buildStore,
		IdempotencyStore:   idempotencyStore,
		WorkspaceStore:     workspaceStore,
		JobStore:           jobStore,
		Users:              users.NewService(db, userStore, roleStore, tokenStore, sessionStore, throttleStore, recorder, keyMaterial),
		Roles:              roles.NewService(db, roleStore, userStore, recorder),
		Tokens:             tokens.NewService(db, tokenStore, recorder, keyMaterial),
		Sessions:           authn.NewSessionService(db, userStore, sessionStore, throttleStore, recorder, keyMaterial, cfg.CanonicalOrigin, SessionAudience),
		Authz:              authz.NewService(roleStore),
		RegistryAuthorizer: registryAuthorizer,
		Zot:                zotClient,
		WorkflowEngine:     workflowEngine,
		Builds:             buildsSvc,
		Devflows:           devflowsSvc,
		BuilderGit:         builderGitSvc,
		Keys:               keyMaterial,
		Audit:              recorder,
	}, nil
}

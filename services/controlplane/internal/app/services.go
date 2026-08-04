package app

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"appliance-code/services/controlplane/internal/appliance"
	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/authn"
	"appliance-code/services/controlplane/internal/authz"
	"appliance-code/services/controlplane/internal/buildergit"
	"appliance-code/services/controlplane/internal/builds"
	"appliance-code/services/controlplane/internal/config"
	"appliance-code/services/controlplane/internal/devflows"
	"appliance-code/services/controlplane/internal/dnsrecords"
	"appliance-code/services/controlplane/internal/keys"
	"appliance-code/services/controlplane/internal/licensing"
	"appliance-code/services/controlplane/internal/logging"
	"appliance-code/services/controlplane/internal/metadatabundle"
	"appliance-code/services/controlplane/internal/notifications"
	"appliance-code/services/controlplane/internal/profiles"
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
	zotAudience           = "zot"
	internalZotSubject    = "appliance-control-plane"
)

type Services struct {
	ApplianceProfile appliance.ResolvedProfile
	Modules          []appliance.ModuleDescriptor

	DB storage.DB

	UserStore          storage.UserStore
	RoleStore          storage.RoleStore
	AuditStore         storage.AuditStore
	RegistryGrantStore storage.RegistryGrantStore
	DNSRecordStore     storage.DNSRecordStore
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
	DNS                *dnsrecords.Service
	Licensing          *licensing.Service
	Metadata           *metadatabundle.Service
	Profiles           *profiles.Service
	Notifications      *notifications.Service

	Keys  *keys.Material
	Audit *audit.Recorder
}

func WireServices(cfg config.Config, logger logging.Logger) (*Services, error) {
	resolved, err := cfg.ResolveProfile()
	if err != nil {
		return nil, fmt.Errorf("app: resolving appliance profile: %w", err)
	}
	return wireServices(cfg, resolved, logger)
}

func wireServices(cfg config.Config, resolved appliance.ResolvedProfile, logger logging.Logger) (*Services, error) {
	resolvedModules, err := cfg.ResolveModules(resolved)
	if err != nil {
		return nil, fmt.Errorf("app: resolving modules: %w", err)
	}
	artifactEnabled := appliance.ModuleEnabled(resolvedModules, appliance.ModuleNameArtifactRegistry)
	buildEnabled := appliance.ModuleEnabled(resolvedModules, appliance.ModuleNameBuild)
	dnsEnabled := appliance.ModuleEnabled(resolvedModules, appliance.ModuleNameLANDNS)
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
	dnsRecordStore := sqlite.NewDNSRecordStore(db)
	licensingStore := sqlite.NewLicensingStore(db)
	metadataStore := sqlite.NewMetadataBundleStore(db)
	recorder := audit.NewRecorder(auditStore)
	licensingSvc := licensing.NewService(db, licensingStore, recorder)
	metadataSvc, err := metadatabundle.NewService(db, metadataStore, recorder, cfg.DataDir)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("app: initializing metadata bundle: %w", err)
	}
	profilesSvc := profiles.NewService(db, licensingStore, licensingSvc, metadataSvc, recorder, string(resolved.Name), profiles.CompleteBundleChecker{})
	notificationsSvc := notifications.NewService(licensingSvc, licensingStore)

	var zotClient zotadapter.Client
	var registryAuthorizer *registryauth.Authorizer
	if artifactEnabled {
		registryAuthorizer = registryauth.NewAuthorizer(registryGrantStore, roleStore)
		if cfg.ZotBaseURL != "" {
			zotClient = zotadapter.NewHTTPClient(cfg.ZotBaseURL, nil, newInternalZotRequestEditor(keyMaterial, cfg.CanonicalOrigin))
		} else if cfg.ZotAllowFake {
			zotClient = zotadapter.NewFake()
		} else {
			db.Close()
			return nil, fmt.Errorf("app: artifact capability requires a real Zot base URL")
		}
	}

	buildStore := sqlite.NewBuildStore(db)
	idempotencyStore := sqlite.NewIdempotencyStore(db)
	workspaceStore := sqlite.NewWorkspaceStore(db)
	jobStore := sqlite.NewJobStore(db)
	var workflowEngine workflows.Engine
	if buildEnabled {
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
	if buildEnabled {
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
		allowedBuilderImages := []string{}
		if digest := strings.TrimSpace(cfg.BuilderImageDigest); digest != "" {
			allowedBuilderImages = []string{digest}
		}
		buildsSvc = builds.NewService(db, buildStore, idempotencyStore, workflowEngine, recorder,
			allowedGitHosts, allowedBuilderImages, cfg.BuildDefaultDeadline,
			cfg.WorkspaceRootDir, cfg.WorkspaceClaimName, builderGitSvc, cfg.BuildCatalog.SensitiveLogValues()...)
		devflowsSvc, err = devflows.NewService(cfg.BuildCatalog, workspaceStore, jobStore, buildsSvc, workflowEngine, cfg.WorkspaceProvisionerImageDigest, cfg.BuilderImageDigest, cfg.WorkspaceRootDir, cfg.WorkspaceClaimName, builderGitSvc, logger, recorder)
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

	var dnsSvc *dnsrecords.Service
	if dnsEnabled {
		var syncer dnsrecords.ZoneSyncer
		if cfg.DNSAllowFakeZoneSync {
			syncer = &dnsrecords.MemoryZoneSyncer{}
		} else {
			var err error
			syncer, err = dnsrecords.NewInClusterConfigMapZoneSyncer(cfg.DNSConfigMapNamespace, cfg.DNSConfigMapName)
			if err != nil {
				db.Close()
				return nil, fmt.Errorf("app: wiring dns zone syncer: %w", err)
			}
		}
		dnsSvc = dnsrecords.NewService(dnsRecordStore, db, syncer, recorder, dnsrecords.Config{
			Zone:               cfg.DNSZoneName,
			ConfigMapNamespace: cfg.DNSConfigMapNamespace,
			ConfigMapName:      cfg.DNSConfigMapName,
			BootstrapHostname:  cfg.DNSBootstrapHostname,
			BootstrapIPv4:      cfg.DNSBootstrapIPv4,
		})
		// Reconcile (and optionally seed) the zone. Default install leaves
		// bootstrap hostname/IP empty so no product A record is created;
		// records come from the DNS API/UI or peer publish.
		if err := dnsSvc.BootstrapSelf(ctx); err != nil {
			db.Close()
			return nil, fmt.Errorf("app: reconciling dns zone: %w", err)
		}
	}

	return &Services{
		ApplianceProfile:   resolved,
		Modules:            resolvedModules,
		DB:                 db,
		UserStore:          userStore,
		RoleStore:          roleStore,
		AuditStore:         auditStore,
		RegistryGrantStore: registryGrantStore,
		DNSRecordStore:     dnsRecordStore,
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
		DNS:                dnsSvc,
		Licensing:          licensingSvc,
		Metadata:           metadataSvc,
		Profiles:           profilesSvc,
		Notifications:      notificationsSvc,
		Keys:               keyMaterial,
		Audit:              recorder,
	}, nil
}

func newInternalZotRequestEditor(keyMaterial *keys.Material, issuer string) func(*http.Request) error {
	return func(req *http.Request) error {
		access, err := internalZotAccess(req.URL.Path)
		if err != nil {
			return err
		}
		if len(access) == 0 {
			return nil
		}
		token, _, err := registryauth.IssueToken(
			keyMaterial.RegistryPrivateKey,
			keyMaterial.RegistryKeyID,
			issuer,
			internalZotSubject,
			zotAudience,
			uuid.Must(uuid.NewV7()).String(),
			access,
		)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	}
}

func internalZotAccess(path string) ([]registryauth.AccessEntry, error) {
	switch {
	case path == "/v2" || path == "/v2/":
		return nil, nil
	case path == "/v2/_catalog":
		// The in-cluster Zot service challenges catalog reads with the
		// repository::pull scope shape. A registry-scoped catalog token is
		// rejected there, even though the public ingress path may allow
		// anonymous catalog reads.
		return []registryauth.AccessEntry{{
			Type:    "repository",
			Name:    "",
			Actions: []string{"pull"},
		}}, nil
	case strings.HasSuffix(path, "/tags/list"):
		return pullAccessForZotPath(path, "/tags/list")
	case strings.Contains(path, "/referrers/"):
		return pullAccessForZotPath(path, "/referrers/")
	default:
		return nil, fmt.Errorf("unsupported zot path %q", path)
	}
}

func pullAccessForZotPath(path, suffix string) ([]registryauth.AccessEntry, error) {
	repoPath := strings.TrimPrefix(path, "/v2/")
	if repoPath == path {
		return nil, fmt.Errorf("unsupported zot path %q", path)
	}
	if strings.HasSuffix(path, suffix) {
		repoPath = strings.TrimSuffix(repoPath, suffix)
	} else {
		idx := strings.Index(repoPath, suffix)
		if idx < 0 {
			return nil, fmt.Errorf("unsupported zot path %q", path)
		}
		repoPath = repoPath[:idx]
	}
	repository, err := url.PathUnescape(repoPath)
	if err != nil {
		return nil, fmt.Errorf("unescape repository path %q: %w", repoPath, err)
	}
	repository, err = registryauth.NormalizeRepositoryName(repository)
	if err != nil {
		return nil, err
	}
	return []registryauth.AccessEntry{{
		Type:    "repository",
		Name:    repository,
		Actions: []string{"pull"},
	}}, nil
}

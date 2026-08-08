package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"

	"appliance-code/services/controlplane/internal/appliance"
	"appliance-code/services/controlplane/internal/logging"
	"appliance-code/services/controlplane/internal/roles"
	"appliance-code/services/controlplane/internal/version"
)

// Deps bundles every handler and shared dependency NewPublicMux needs to
// wire the v1 identity HTTP surface.
type Deps struct {
	Logger           logging.Logger
	Auth             AuthDeps
	AuthH            *AuthHandlers
	SetupH           *SetupHandlers
	CapabilitiesH    *CapabilitiesHandlers
	IdentityH        *IdentityHandlers
	ForwardAuthH     *ForwardAuthHandlers
	UsersH           *UserHandlers
	RolesH           *RoleHandlers
	TokensH          *TokenHandlers
	RegistryH        *RegistryTokenHandlers
	RegistryGrantsH  *RegistryGrantHandlers
	RegistryCatalogH *RegistryCatalogHandlers
	FilesH           *FileHandlers
	DNSH             *DNSHandlers
	LANDNSPublishH   *LANDNSPublishHandlers
	BuildsH          *BuildHandlers
	DevflowsH        *DeveloperWorkflowHandlers
	LicensingH       *LicensingHandlers
	SetupStateH      *SetupStateHandlers
	NotificationsH   *NotificationHandlers
	ProfilesH        *ProfileHandlers
	MetadataH        *MetadataBundleHandlers
	MCPHandler       http.Handler
	ProxiedServices  []ServiceProxyRegistration
}

type publicRoute struct {
	capability appliance.Capability
	moduleName string
	pattern    string
	build      func(Deps, wrappers) (http.Handler, error)
}

type wrappers struct {
	protect           func(permission string, h http.HandlerFunc) http.Handler
	protectAny        func(h http.HandlerFunc, permissions ...string) http.Handler
	authenticatedOnly func(h http.HandlerFunc) http.Handler
}

// NewPublicMux builds the mux for the public-facing listener: the Phase 2
// auth/user/role/token surface, protected by RequireAuth and per-route
// RequirePermission/RequireAnyPermission. Everything else falls through to
// a standard application/problem+json 404.
func NewPublicMux(deps Deps, capabilities appliance.Set, modules []appliance.ModuleDescriptor) (http.Handler, error) {
	if deps.Logger == nil {
		return nil, fmt.Errorf("logger is required")
	}
	mux := http.NewServeMux()

	authRequired := RequireAuth(deps.Auth)
	w := wrappers{
		protect: func(permission string, h http.HandlerFunc) http.Handler {
			return authRequired(RequirePermission(permission)(h))
		},
		protectAny: func(h http.HandlerFunc, permissions ...string) http.Handler {
			return authRequired(RequireAnyPermission(permissions...)(h))
		},
		authenticatedOnly: func(h http.HandlerFunc) http.Handler {
			return authRequired(h)
		},
	}

	for _, route := range append(publicRoutes(), proxiedServiceRoutes(deps.ProxiedServices)...) {
		if route.moduleName != "" {
			if !appliance.ModuleEnabled(modules, route.moduleName) {
				continue
			}
		} else if !capabilities.Enabled(route.capability) {
			continue
		}
		handler, err := route.build(deps, w)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", route.pattern, err)
		}
		mux.Handle(route.pattern, handler)
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		WriteProblem(w, r, http.StatusNotFound, "not_found", "Not found", "")
	})

	chain := Chain(TraceID, RequestID, AccessLog(deps.Logger), APIExchangeLog(deps.Logger), Recover(deps.Logger))
	return chain(mux), nil
}

// NewInternalMux builds the mux for the operator-only internal listener:
// health probes and version metadata. It must never be exposed through
// public ingress.
func NewInternalMux(logger logging.Logger, checker ReadinessChecker, startup *StartupState) http.Handler {
	if logger == nil {
		panic("httpapi.NewInternalMux: logger is required")
	}
	mux := http.NewServeMux()
	RegisterHealthRoutes(mux, logger, checker, startup)

	mux.HandleFunc("GET /version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(version.Current())
	})

	chain := Chain(TraceID, RequestID, AccessLog(logger), Recover(logger))
	return chain(mux)
}

func publicRoutes() []publicRoute {
	return []publicRoute{
		{capability: appliance.CapabilityBase, pattern: "GET /api/v1/setup/status", build: func(deps Deps, _ wrappers) (http.Handler, error) {
			if deps.SetupH == nil {
				return nil, fmt.Errorf("missing setup handlers")
			}
			return http.HandlerFunc(deps.SetupH.Status), nil
		}},
		// Reports the whole resolved capability set (not the profile
		// name), so a caller never needs its own copy of profileCatalog
		// to decide what to show for this appliance instance. Always
		// registered: CapabilityBase is present in every profile.
		{capability: appliance.CapabilityBase, pattern: "GET /api/v1/capabilities", build: func(deps Deps, _ wrappers) (http.Handler, error) {
			if deps.CapabilitiesH == nil {
				return nil, fmt.Errorf("missing capabilities handlers")
			}
			return http.HandlerFunc(deps.CapabilitiesH.Get), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "GET /api/v1/appliance/identity", build: func(deps Deps, _ wrappers) (http.Handler, error) {
			if deps.IdentityH == nil {
				return nil, fmt.Errorf("missing identity handlers")
			}
			return http.HandlerFunc(deps.IdentityH.Get), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "POST /api/v1/setup/first-admin", build: func(deps Deps, _ wrappers) (http.Handler, error) {
			if deps.SetupH == nil {
				return nil, fmt.Errorf("missing setup handlers")
			}
			return http.HandlerFunc(deps.SetupH.CreateFirstAdmin), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "POST /api/v1/auth/login", build: func(deps Deps, _ wrappers) (http.Handler, error) {
			if deps.AuthH == nil {
				return nil, fmt.Errorf("missing auth handlers")
			}
			return http.HandlerFunc(deps.AuthH.Login), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "POST /api/v1/auth/refresh", build: func(deps Deps, _ wrappers) (http.Handler, error) {
			if deps.AuthH == nil {
				return nil, fmt.Errorf("missing auth handlers")
			}
			return http.HandlerFunc(deps.AuthH.Refresh), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "POST /api/v1/auth/logout", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.AuthH == nil {
				return nil, fmt.Errorf("missing auth handlers")
			}
			return w.authenticatedOnly(deps.AuthH.Logout), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "POST /api/v1/auth/password", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.AuthH == nil {
				return nil, fmt.Errorf("missing auth handlers")
			}
			return w.authenticatedOnly(deps.AuthH.ChangePassword), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "GET /api/v1/auth/session", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.AuthH == nil {
				return nil, fmt.Errorf("missing auth handlers")
			}
			return w.authenticatedOnly(deps.AuthH.Session), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "GET /api/v1/licensing/status", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.LicensingH == nil {
				return nil, fmt.Errorf("missing licensing handlers")
			}
			return w.protect(roles.PermLicensingRead, deps.LicensingH.Status), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "GET /api/v1/licensing/entitlements", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.LicensingH == nil {
				return nil, fmt.Errorf("missing licensing handlers")
			}
			return w.protect(roles.PermLicensingRead, deps.LicensingH.Entitlements), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "PUT /api/v1/licensing/license", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.LicensingH == nil {
				return nil, fmt.Errorf("missing licensing handlers")
			}
			return w.protect(roles.PermLicensingManage, deps.LicensingH.ImportLicense), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "POST /api/v1/licensing/base-entitlement/accept", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.LicensingH == nil {
				return nil, fmt.Errorf("missing licensing handlers")
			}
			return w.protect(roles.PermLicensingManage, deps.LicensingH.AcceptBase), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "GET /api/v1/appliance/setup-state", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.SetupStateH == nil {
				return nil, fmt.Errorf("missing setup-state handlers")
			}
			return w.authenticatedOnly(deps.SetupStateH.Get), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "GET /api/v1/notifications", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.NotificationsH == nil {
				return nil, fmt.Errorf("missing notification handlers")
			}
			return w.protect(roles.PermNotificationsRead, deps.NotificationsH.List), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "POST /api/v1/notifications/{id}/acknowledge", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.NotificationsH == nil {
				return nil, fmt.Errorf("missing notification handlers")
			}
			return w.protect(roles.PermNotificationsAcknowledge, deps.NotificationsH.Acknowledge), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "GET /api/v1/appliance/capabilities", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.ProfilesH == nil {
				return nil, fmt.Errorf("missing profile handlers")
			}
			return w.protect(roles.PermProfilesRead, deps.ProfilesH.ListCapabilities), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "GET /api/v1/appliance/profiles", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.ProfilesH == nil {
				return nil, fmt.Errorf("missing profile handlers")
			}
			return w.protect(roles.PermProfilesRead, deps.ProfilesH.List), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "GET /api/v1/appliance/profiles/{profileId}", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.ProfilesH == nil {
				return nil, fmt.Errorf("missing profile handlers")
			}
			return w.protect(roles.PermProfilesRead, deps.ProfilesH.Get), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "POST /api/v1/appliance/profiles/{profileId}/validate", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.ProfilesH == nil {
				return nil, fmt.Errorf("missing profile handlers")
			}
			return w.protect(roles.PermProfilesActivate, deps.ProfilesH.Validate), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "POST /api/v1/appliance/profiles/{profileId}/activate", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.ProfilesH == nil {
				return nil, fmt.Errorf("missing profile handlers")
			}
			return w.protect(roles.PermProfilesActivate, deps.ProfilesH.Activate), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "GET /api/v1/appliance/metadata-bundle", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.MetadataH == nil {
				return nil, fmt.Errorf("missing metadata-bundle handlers")
			}
			return w.protect(roles.PermMetadataRead, deps.MetadataH.Status), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "POST /api/v1/appliance/metadata-bundle/validate", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.MetadataH == nil {
				return nil, fmt.Errorf("missing metadata-bundle handlers")
			}
			return w.protect(roles.PermMetadataManage, deps.MetadataH.Validate), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "POST /api/v1/appliance/metadata-bundle/install", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.MetadataH == nil {
				return nil, fmt.Errorf("missing metadata-bundle handlers")
			}
			return w.protect(roles.PermMetadataManage, deps.MetadataH.Install), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "POST /api/v1/appliance/metadata-bundle/rollback", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.MetadataH == nil {
				return nil, fmt.Errorf("missing metadata-bundle handlers")
			}
			return w.protect(roles.PermMetadataManage, deps.MetadataH.Rollback), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "/internal/auth/check", build: func(deps Deps, _ wrappers) (http.Handler, error) {
			if deps.ForwardAuthH == nil {
				return nil, fmt.Errorf("missing forward-auth handlers")
			}
			return http.HandlerFunc(deps.ForwardAuthH.Check), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "POST /api/v1/users", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.UsersH == nil {
				return nil, fmt.Errorf("missing user handlers")
			}
			return w.protect(roles.PermUsersCreate, deps.UsersH.Create), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "GET /api/v1/users", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.UsersH == nil {
				return nil, fmt.Errorf("missing user handlers")
			}
			return w.protect(roles.PermUsersRead, deps.UsersH.List), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "GET /api/v1/users/{id}", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.UsersH == nil {
				return nil, fmt.Errorf("missing user handlers")
			}
			return w.protect(roles.PermUsersRead, deps.UsersH.Get), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "PATCH /api/v1/users/{id}", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.UsersH == nil {
				return nil, fmt.Errorf("missing user handlers")
			}
			return w.protect(roles.PermUsersUpdate, deps.UsersH.Patch), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "POST /api/v1/users/{id}/disable", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.UsersH == nil {
				return nil, fmt.Errorf("missing user handlers")
			}
			return w.protect(roles.PermUsersDisable, deps.UsersH.Disable), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "POST /api/v1/users/{id}/enable", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.UsersH == nil {
				return nil, fmt.Errorf("missing user handlers")
			}
			return w.protect(roles.PermUsersDisable, deps.UsersH.Enable), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "POST /api/v1/users/{id}/unlock", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.UsersH == nil {
				return nil, fmt.Errorf("missing user handlers")
			}
			return w.protect(roles.PermUsersDisable, deps.UsersH.Unlock), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "POST /api/v1/users/{id}/password-reset", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.UsersH == nil {
				return nil, fmt.Errorf("missing user handlers")
			}
			return w.protect(roles.PermUsersUpdate, deps.UsersH.PasswordReset), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "PUT /api/v1/users/{id}/roles", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.UsersH == nil {
				return nil, fmt.Errorf("missing user handlers")
			}
			return w.protect(roles.PermUsersUpdate, deps.UsersH.SetRoles), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "GET /api/v1/roles", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.RolesH == nil {
				return nil, fmt.Errorf("missing role handlers")
			}
			return w.protect(roles.PermRolesRead, deps.RolesH.List), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "GET /api/v1/permissions", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.RolesH == nil {
				return nil, fmt.Errorf("missing role handlers")
			}
			return w.protect(roles.PermRolesRead, deps.RolesH.ListPermissions), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "POST /api/v1/roles", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.RolesH == nil {
				return nil, fmt.Errorf("missing role handlers")
			}
			return w.protect(roles.PermRolesCreate, deps.RolesH.Create), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "PUT /api/v1/roles/{id}", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.RolesH == nil {
				return nil, fmt.Errorf("missing role handlers")
			}
			return w.protect(roles.PermRolesUpdate, deps.RolesH.Update), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "DELETE /api/v1/roles/{id}", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.RolesH == nil {
				return nil, fmt.Errorf("missing role handlers")
			}
			return w.protect(roles.PermRolesDelete, deps.RolesH.Delete), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "POST /api/v1/tokens", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.TokensH == nil {
				return nil, fmt.Errorf("missing token handlers")
			}
			return w.protect(roles.PermTokensCreateSelf, deps.TokensH.CreateSelf), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "GET /api/v1/tokens", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.TokensH == nil {
				return nil, fmt.Errorf("missing token handlers")
			}
			return w.protect(roles.PermTokensReadSelf, deps.TokensH.ListSelf), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "DELETE /api/v1/tokens/{id}", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.TokensH == nil {
				return nil, fmt.Errorf("missing token handlers")
			}
			return w.protectAny(deps.TokensH.RevokeSelf, roles.PermTokensRevokeSelf, roles.PermTokensRevokeAny), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "POST /api/v1/users/{userId}/tokens", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.TokensH == nil {
				return nil, fmt.Errorf("missing token handlers")
			}
			return w.protect(roles.PermTokensCreateAny, deps.TokensH.CreateForUser), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "DELETE /api/v1/users/{userId}/tokens/{tokenId}", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.TokensH == nil {
				return nil, fmt.Errorf("missing token handlers")
			}
			return w.protect(roles.PermTokensRevokeAny, deps.TokensH.RevokeForUser), nil
		}},
		{capability: appliance.CapabilityBase, pattern: "/mcp", build: func(deps Deps, _ wrappers) (http.Handler, error) {
			if deps.MCPHandler == nil {
				return nil, fmt.Errorf("missing MCP handler")
			}
			return deps.MCPHandler, nil
		}},
		// Outbound publish to a remote DNS appliance. Available on every
		// profile (base); does not require local dns capability.
		{capability: appliance.CapabilityBase, pattern: "POST /api/v1/dns/publish", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.LANDNSPublishH == nil {
				return nil, fmt.Errorf("missing dns publish handlers")
			}
			return w.protect(roles.PermDNSPublish, deps.LANDNSPublishH.Publish), nil
		}},
		{moduleName: appliance.ModuleNameArtifactRegistry, pattern: "GET /api/v1/registry/token", build: func(deps Deps, _ wrappers) (http.Handler, error) {
			if deps.RegistryH == nil {
				return nil, fmt.Errorf("missing registry token handlers")
			}
			return http.HandlerFunc(deps.RegistryH.Token), nil
		}},
		{moduleName: appliance.ModuleNameArtifactRegistry, pattern: "GET /api/v1/registry/grants", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.RegistryGrantsH == nil {
				return nil, fmt.Errorf("missing registry grant handlers")
			}
			return w.protect(roles.PermArtifactsGrantsRead, deps.RegistryGrantsH.List), nil
		}},
		{moduleName: appliance.ModuleNameArtifactRegistry, pattern: "POST /api/v1/registry/grants", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.RegistryGrantsH == nil {
				return nil, fmt.Errorf("missing registry grant handlers")
			}
			return w.protect(roles.PermArtifactsGrantsWrite, deps.RegistryGrantsH.Create), nil
		}},
		{moduleName: appliance.ModuleNameArtifactRegistry, pattern: "DELETE /api/v1/registry/grants/{id}", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.RegistryGrantsH == nil {
				return nil, fmt.Errorf("missing registry grant handlers")
			}
			return w.protect(roles.PermArtifactsGrantsWrite, deps.RegistryGrantsH.Delete), nil
		}},
		{moduleName: appliance.ModuleNameArtifactRegistry, pattern: "GET /api/v1/registry/repositories", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.RegistryCatalogH == nil {
				return nil, fmt.Errorf("missing registry catalog handlers")
			}
			return w.authenticatedOnly(deps.RegistryCatalogH.ListRepositories), nil
		}},
		{moduleName: appliance.ModuleNameArtifactRegistry, pattern: "GET /api/v1/registry/repositories/{rest...}", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.RegistryCatalogH == nil {
				return nil, fmt.Errorf("missing registry catalog handlers")
			}
			return w.authenticatedOnly(deps.RegistryCatalogH.CatalogItem), nil
		}},
		{moduleName: appliance.ModuleNameFiles, pattern: "GET /api/v1/files", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.FilesH == nil {
				return nil, fmt.Errorf("missing file handlers")
			}
			return w.protect(roles.PermFilesRead, deps.FilesH.Get), nil
		}},
		{moduleName: appliance.ModuleNameFiles, pattern: "GET /api/v1/files/{rest...}", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.FilesH == nil {
				return nil, fmt.Errorf("missing file handlers")
			}
			return w.protect(roles.PermFilesRead, deps.FilesH.Get), nil
		}},
		{moduleName: appliance.ModuleNameFiles, pattern: "POST /api/v1/files/{rest...}", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.FilesH == nil {
				return nil, fmt.Errorf("missing file handlers")
			}
			return w.protect(roles.PermFilesWrite, deps.FilesH.Upload), nil
		}},
		{moduleName: appliance.ModuleNameLANDNS, pattern: "GET /api/v1/dns/records", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.DNSH == nil {
				return nil, fmt.Errorf("missing dns handlers")
			}
			return w.protect(roles.PermDNSRecordsRead, deps.DNSH.List), nil
		}},
		{moduleName: appliance.ModuleNameLANDNS, pattern: "PUT /api/v1/dns/records/{name}", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.DNSH == nil {
				return nil, fmt.Errorf("missing dns handlers")
			}
			return w.protectAny(deps.DNSH.Upsert, roles.PermDNSRecordsWrite, roles.PermDNSRecordsRegister), nil
		}},
		{moduleName: appliance.ModuleNameLANDNS, pattern: "DELETE /api/v1/dns/records/{name}", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.DNSH == nil {
				return nil, fmt.Errorf("missing dns handlers")
			}
			return w.protect(roles.PermDNSRecordsWrite, deps.DNSH.Delete), nil
		}},
		{moduleName: appliance.ModuleNameBuild, pattern: "GET /api/v1/work-profiles", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.DevflowsH == nil {
				return nil, fmt.Errorf("missing developer workflow handlers")
			}
			return w.protect(roles.PermWorkProfilesRead, deps.DevflowsH.ListWorkProfiles), nil
		}},
		{moduleName: appliance.ModuleNameBuild, pattern: "GET /api/v1/builder/catalog", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.DevflowsH == nil {
				return nil, fmt.Errorf("missing developer workflow handlers")
			}
			return w.protect(roles.PermWorkProfilesRead, deps.DevflowsH.GetBuilderCatalog), nil
		}},
		{moduleName: appliance.ModuleNameBuild, pattern: "PUT /api/v1/builder/catalog", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.DevflowsH == nil {
				return nil, fmt.Errorf("missing developer workflow handlers")
			}
			return w.protect(roles.PermSystemOperate, deps.DevflowsH.UpdateBuilderCatalog), nil
		}},
		{moduleName: appliance.ModuleNameBuild, pattern: "GET /api/v1/builder/git-access", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.DevflowsH == nil {
				return nil, fmt.Errorf("missing developer workflow handlers")
			}
			return w.protect(roles.PermWorkProfilesRead, deps.DevflowsH.GetBuilderGitAccess), nil
		}},
		{moduleName: appliance.ModuleNameBuild, pattern: "PUT /api/v1/builder/git-access/{name}", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.DevflowsH == nil {
				return nil, fmt.Errorf("missing developer workflow handlers")
			}
			return w.protect(roles.PermSystemOperate, deps.DevflowsH.UpdateBuilderGitAccess), nil
		}},
		{moduleName: appliance.ModuleNameBuild, pattern: "DELETE /api/v1/builder/git-access/{name}", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.DevflowsH == nil {
				return nil, fmt.Errorf("missing developer workflow handlers")
			}
			return w.protect(roles.PermSystemOperate, deps.DevflowsH.DeleteBuilderGitAccess), nil
		}},
		{moduleName: appliance.ModuleNameBuild, pattern: "POST /api/v1/workspaces", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.DevflowsH == nil {
				return nil, fmt.Errorf("missing developer workflow handlers")
			}
			return w.protect(roles.PermWorkspacesCreate, deps.DevflowsH.CreateWorkspace), nil
		}},
		{moduleName: appliance.ModuleNameBuild, pattern: "GET /api/v1/workspaces", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.DevflowsH == nil {
				return nil, fmt.Errorf("missing developer workflow handlers")
			}
			return w.protectAny(deps.DevflowsH.ListWorkspaces, roles.PermWorkspacesReadSelf, roles.PermWorkspacesReadAny), nil
		}},
		{moduleName: appliance.ModuleNameBuild, pattern: "GET /api/v1/workspaces/{workspaceId}", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.DevflowsH == nil {
				return nil, fmt.Errorf("missing developer workflow handlers")
			}
			return w.protectAny(deps.DevflowsH.GetWorkspace, roles.PermWorkspacesReadSelf, roles.PermWorkspacesReadAny), nil
		}},
		{moduleName: appliance.ModuleNameBuild, pattern: "DELETE /api/v1/workspaces/{workspaceId}", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.DevflowsH == nil {
				return nil, fmt.Errorf("missing developer workflow handlers")
			}
			return w.protectAny(deps.DevflowsH.DeleteWorkspace, roles.PermWorkspacesDeleteSelf, roles.PermWorkspacesDeleteAny), nil
		}},
		{moduleName: appliance.ModuleNameBuild, pattern: "GET /api/v1/current-workspace", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.DevflowsH == nil {
				return nil, fmt.Errorf("missing developer workflow handlers")
			}
			return w.protect(roles.PermWorkspacesReadSelf, deps.DevflowsH.GetCurrentWorkspace), nil
		}},
		{moduleName: appliance.ModuleNameBuild, pattern: "POST /api/v1/current-workspace", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.DevflowsH == nil {
				return nil, fmt.Errorf("missing developer workflow handlers")
			}
			return w.protect(roles.PermWorkspacesReadSelf, deps.DevflowsH.SetCurrentWorkspace), nil
		}},
		{moduleName: appliance.ModuleNameBuild, pattern: "GET /api/v1/current-workspace/build-targets", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.DevflowsH == nil {
				return nil, fmt.Errorf("missing developer workflow handlers")
			}
			return w.protect(roles.PermBuildTargetsRead, deps.DevflowsH.ListCurrentBuildTargets), nil
		}},
		{moduleName: appliance.ModuleNameBuild, pattern: "POST /api/v1/current-workspace/builds", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.DevflowsH == nil {
				return nil, fmt.Errorf("missing developer workflow handlers")
			}
			return w.protect(roles.PermBuildsCreate, deps.DevflowsH.SubmitCurrentBuild), nil
		}},
		{moduleName: appliance.ModuleNameBuild, pattern: "GET /api/v1/current-workspace/build-status", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.DevflowsH == nil {
				return nil, fmt.Errorf("missing developer workflow handlers")
			}
			return w.protect(roles.PermJobsReadSelf, deps.DevflowsH.CurrentWorkspaceBuildStatus), nil
		}},
		{moduleName: appliance.ModuleNameBuild, pattern: "GET /api/v1/jobs", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.DevflowsH == nil {
				return nil, fmt.Errorf("missing developer workflow handlers")
			}
			return w.protectAny(deps.DevflowsH.ListJobs, roles.PermJobsReadSelf, roles.PermJobsReadAny), nil
		}},
		{moduleName: appliance.ModuleNameBuild, pattern: "GET /api/v1/jobs/{jobId}", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.DevflowsH == nil {
				return nil, fmt.Errorf("missing developer workflow handlers")
			}
			return w.protectAny(deps.DevflowsH.GetJob, roles.PermJobsReadSelf, roles.PermJobsReadAny), nil
		}},
		{moduleName: appliance.ModuleNameBuild, pattern: "POST /api/v1/jobs/{jobId}/cancel", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.DevflowsH == nil {
				return nil, fmt.Errorf("missing developer workflow handlers")
			}
			return w.protectAny(deps.DevflowsH.CancelJob, roles.PermJobsCancelSelf, roles.PermJobsCancelAny), nil
		}},
		{moduleName: appliance.ModuleNameBuild, pattern: "GET /api/v1/jobs/{jobId}/steps", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.DevflowsH == nil {
				return nil, fmt.Errorf("missing developer workflow handlers")
			}
			return w.protectAny(deps.DevflowsH.JobSteps, roles.PermJobsReadSelf, roles.PermJobsReadAny), nil
		}},
		{moduleName: appliance.ModuleNameBuild, pattern: "GET /api/v1/jobs/{jobId}/logs", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.DevflowsH == nil {
				return nil, fmt.Errorf("missing developer workflow handlers")
			}
			return w.protectAny(deps.DevflowsH.JobLogs, roles.PermJobsReadSelf, roles.PermJobsReadAny), nil
		}},
		{moduleName: appliance.ModuleNameBuild, pattern: "POST /api/v1/builds", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.BuildsH == nil {
				return nil, fmt.Errorf("missing build handlers")
			}
			return w.protect(roles.PermBuildsCreate, deps.BuildsH.Create), nil
		}},
		{moduleName: appliance.ModuleNameBuild, pattern: "GET /api/v1/builds", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.BuildsH == nil {
				return nil, fmt.Errorf("missing build handlers")
			}
			return w.protectAny(deps.BuildsH.List, roles.PermBuildsReadSelf, roles.PermBuildsReadAny), nil
		}},
		{moduleName: appliance.ModuleNameBuild, pattern: "GET /api/v1/builds/{id}", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.BuildsH == nil {
				return nil, fmt.Errorf("missing build handlers")
			}
			return w.protectAny(deps.BuildsH.Get, roles.PermBuildsReadSelf, roles.PermBuildsReadAny), nil
		}},
		{moduleName: appliance.ModuleNameBuild, pattern: "POST /api/v1/builds/{id}/cancel", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.BuildsH == nil {
				return nil, fmt.Errorf("missing build handlers")
			}
			return w.protectAny(deps.BuildsH.Cancel, roles.PermBuildsCancelSelf, roles.PermBuildsCancelAny), nil
		}},
		{moduleName: appliance.ModuleNameBuild, pattern: "GET /api/v1/builds/{id}/logs", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.BuildsH == nil {
				return nil, fmt.Errorf("missing build handlers")
			}
			return w.protectAny(deps.BuildsH.Logs, roles.PermBuildsReadSelf, roles.PermBuildsReadAny), nil
		}},
	}
}

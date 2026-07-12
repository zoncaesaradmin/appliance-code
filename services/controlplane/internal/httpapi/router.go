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
	ForwardAuthH     *ForwardAuthHandlers
	UsersH           *UserHandlers
	RolesH           *RoleHandlers
	TokensH          *TokenHandlers
	RegistryH        *RegistryTokenHandlers
	RegistryGrantsH  *RegistryGrantHandlers
	RegistryCatalogH *RegistryCatalogHandlers
	BuildsH          *BuildHandlers
	MCPHandler       http.Handler
}

type publicRoute struct {
	capability appliance.Capability
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
func NewPublicMux(deps Deps, capabilities appliance.Set) (http.Handler, error) {
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

	for _, route := range publicRoutes() {
		if !capabilities.Enabled(route.capability) {
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

	chain := Chain(RequestID, Recover(deps.Logger), AccessLog(deps.Logger))
	return chain(mux), nil
}

// NewInternalMux builds the mux for the operator-only internal listener:
// health probes and version metadata. It must never be exposed through
// public ingress.
func NewInternalMux(logger logging.Logger, checker ReadinessChecker, startup *StartupState) http.Handler {
	mux := http.NewServeMux()
	RegisterHealthRoutes(mux, checker, startup)

	mux.HandleFunc("GET /version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(version.Current())
	})

	chain := Chain(RequestID, Recover(logger), AccessLog(logger))
	return chain(mux)
}

func publicRoutes() []publicRoute {
	return []publicRoute{
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
		{capability: appliance.CapabilityBase, pattern: "GET /api/v1/auth/session", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.AuthH == nil {
				return nil, fmt.Errorf("missing auth handlers")
			}
			return w.authenticatedOnly(deps.AuthH.Session), nil
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
		{capability: appliance.CapabilityArtifact, pattern: "GET /api/v1/registry/token", build: func(deps Deps, _ wrappers) (http.Handler, error) {
			if deps.RegistryH == nil {
				return nil, fmt.Errorf("missing registry token handlers")
			}
			return http.HandlerFunc(deps.RegistryH.Token), nil
		}},
		{capability: appliance.CapabilityArtifact, pattern: "GET /api/v1/registry/grants", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.RegistryGrantsH == nil {
				return nil, fmt.Errorf("missing registry grant handlers")
			}
			return w.protect(roles.PermRegistryGrantsRead, deps.RegistryGrantsH.List), nil
		}},
		{capability: appliance.CapabilityArtifact, pattern: "POST /api/v1/registry/grants", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.RegistryGrantsH == nil {
				return nil, fmt.Errorf("missing registry grant handlers")
			}
			return w.protect(roles.PermRegistryGrantsWrite, deps.RegistryGrantsH.Create), nil
		}},
		{capability: appliance.CapabilityArtifact, pattern: "DELETE /api/v1/registry/grants/{id}", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.RegistryGrantsH == nil {
				return nil, fmt.Errorf("missing registry grant handlers")
			}
			return w.protect(roles.PermRegistryGrantsWrite, deps.RegistryGrantsH.Delete), nil
		}},
		{capability: appliance.CapabilityArtifact, pattern: "GET /api/v1/registry/repositories", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.RegistryCatalogH == nil {
				return nil, fmt.Errorf("missing registry catalog handlers")
			}
			return w.authenticatedOnly(deps.RegistryCatalogH.ListRepositories), nil
		}},
		{capability: appliance.CapabilityArtifact, pattern: "GET /api/v1/registry/repositories/{rest...}", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.RegistryCatalogH == nil {
				return nil, fmt.Errorf("missing registry catalog handlers")
			}
			return w.authenticatedOnly(deps.RegistryCatalogH.CatalogItem), nil
		}},
		{capability: appliance.CapabilityBuild, pattern: "POST /api/v1/builds", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.BuildsH == nil {
				return nil, fmt.Errorf("missing build handlers")
			}
			return w.protect(roles.PermBuildsCreate, deps.BuildsH.Create), nil
		}},
		{capability: appliance.CapabilityBuild, pattern: "GET /api/v1/builds", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.BuildsH == nil {
				return nil, fmt.Errorf("missing build handlers")
			}
			return w.protectAny(deps.BuildsH.List, roles.PermBuildsReadSelf, roles.PermBuildsReadAny), nil
		}},
		{capability: appliance.CapabilityBuild, pattern: "GET /api/v1/builds/{id}", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.BuildsH == nil {
				return nil, fmt.Errorf("missing build handlers")
			}
			return w.protectAny(deps.BuildsH.Get, roles.PermBuildsReadSelf, roles.PermBuildsReadAny), nil
		}},
		{capability: appliance.CapabilityBuild, pattern: "POST /api/v1/builds/{id}/cancel", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.BuildsH == nil {
				return nil, fmt.Errorf("missing build handlers")
			}
			return w.protectAny(deps.BuildsH.Cancel, roles.PermBuildsCancelSelf, roles.PermBuildsCancelAny), nil
		}},
		{capability: appliance.CapabilityBuild, pattern: "GET /api/v1/builds/{id}/logs", build: func(deps Deps, w wrappers) (http.Handler, error) {
			if deps.BuildsH == nil {
				return nil, fmt.Errorf("missing build handlers")
			}
			return w.protectAny(deps.BuildsH.Logs, roles.PermBuildsReadSelf, roles.PermBuildsReadAny), nil
		}},
	}
}

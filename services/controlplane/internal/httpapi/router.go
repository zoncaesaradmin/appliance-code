package httpapi

import (
	"encoding/json"
	"net/http"

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

// NewPublicMux builds the mux for the public-facing listener: the Phase 2
// auth/user/role/token surface, protected by RequireAuth and per-route
// RequirePermission/RequireAnyPermission. Everything else falls through to
// a standard application/problem+json 404.
func NewPublicMux(deps Deps) http.Handler {
	mux := http.NewServeMux()

	authRequired := RequireAuth(deps.Auth)
	protect := func(permission string, h http.HandlerFunc) http.Handler {
		return authRequired(RequirePermission(permission)(h))
	}
	protectAny := func(h http.HandlerFunc, permissions ...string) http.Handler {
		return authRequired(RequireAnyPermission(permissions...)(h))
	}
	authenticatedOnly := func(h http.HandlerFunc) http.Handler {
		return authRequired(h)
	}

	// Authentication
	mux.HandleFunc("POST /api/v1/auth/login", deps.AuthH.Login)
	mux.HandleFunc("POST /api/v1/auth/refresh", deps.AuthH.Refresh)
	mux.Handle("POST /api/v1/auth/logout", authenticatedOnly(deps.AuthH.Logout))
	mux.Handle("GET /api/v1/auth/session", authenticatedOnly(deps.AuthH.Session))
	if deps.ForwardAuthH != nil {
		mux.HandleFunc("/internal/auth/check", deps.ForwardAuthH.Check)
	}

	// User management
	mux.Handle("POST /api/v1/users", protect(roles.PermUsersCreate, deps.UsersH.Create))
	mux.Handle("GET /api/v1/users", protect(roles.PermUsersRead, deps.UsersH.List))
	mux.Handle("GET /api/v1/users/{id}", protect(roles.PermUsersRead, deps.UsersH.Get))
	mux.Handle("PATCH /api/v1/users/{id}", protect(roles.PermUsersUpdate, deps.UsersH.Patch))
	mux.Handle("POST /api/v1/users/{id}/disable", protect(roles.PermUsersDisable, deps.UsersH.Disable))
	mux.Handle("POST /api/v1/users/{id}/enable", protect(roles.PermUsersDisable, deps.UsersH.Enable))
	mux.Handle("POST /api/v1/users/{id}/unlock", protect(roles.PermUsersDisable, deps.UsersH.Unlock))
	mux.Handle("POST /api/v1/users/{id}/password-reset", protect(roles.PermUsersUpdate, deps.UsersH.PasswordReset))
	mux.Handle("PUT /api/v1/users/{id}/roles", protect(roles.PermUsersUpdate, deps.UsersH.SetRoles))

	// Role management
	mux.Handle("GET /api/v1/roles", protect(roles.PermRolesRead, deps.RolesH.List))
	mux.Handle("GET /api/v1/permissions", protect(roles.PermRolesRead, deps.RolesH.ListPermissions))
	mux.Handle("POST /api/v1/roles", protect(roles.PermRolesCreate, deps.RolesH.Create))
	mux.Handle("PUT /api/v1/roles/{id}", protect(roles.PermRolesUpdate, deps.RolesH.Update))
	mux.Handle("DELETE /api/v1/roles/{id}", protect(roles.PermRolesDelete, deps.RolesH.Delete))

	// API token management
	mux.Handle("POST /api/v1/tokens", protect(roles.PermTokensCreateSelf, deps.TokensH.CreateSelf))
	mux.Handle("GET /api/v1/tokens", protect(roles.PermTokensReadSelf, deps.TokensH.ListSelf))
	mux.Handle("DELETE /api/v1/tokens/{id}", protectAny(deps.TokensH.RevokeSelf, roles.PermTokensRevokeSelf, roles.PermTokensRevokeAny))
	mux.Handle("POST /api/v1/users/{userId}/tokens", protect(roles.PermTokensCreateAny, deps.TokensH.CreateForUser))
	mux.Handle("DELETE /api/v1/users/{userId}/tokens/{tokenId}", protect(roles.PermTokensRevokeAny, deps.TokensH.RevokeForUser))

	// Registry token/grants. The token endpoint authenticates itself (an
	// OCI Distribution client presents Basic auth, not a bearer session);
	// it is not wrapped in RequireAuth.
	if deps.RegistryH != nil {
		mux.HandleFunc("GET /api/v1/registry/token", deps.RegistryH.Token)
	}
	if deps.RegistryGrantsH != nil {
		mux.Handle("GET /api/v1/registry/grants", protect(roles.PermRegistryGrantsRead, deps.RegistryGrantsH.List))
		mux.Handle("POST /api/v1/registry/grants", protect(roles.PermRegistryGrantsWrite, deps.RegistryGrantsH.Create))
		mux.Handle("DELETE /api/v1/registry/grants/{id}", protect(roles.PermRegistryGrantsWrite, deps.RegistryGrantsH.Delete))
	}
	// Repository/tag/referrer catalog reads: authentication is required at
	// the route level, but which repositories are visible is a per-request
	// grant-filtering decision the handler makes itself (registry.pull plus
	// prefix matching), not a single fixed permission gate.
	if deps.RegistryCatalogH != nil {
		mux.Handle("GET /api/v1/registry/repositories", authenticatedOnly(deps.RegistryCatalogH.ListRepositories))
		mux.Handle("GET /api/v1/registry/repositories/{rest...}", authenticatedOnly(deps.RegistryCatalogH.CatalogItem))
	}

	// Build management: create requires the base permission; read/cancel/
	// logs require either the ".self" or ".any" permission at the route
	// level, with the handler enforcing per-build ownership itself.
	if deps.BuildsH != nil {
		mux.Handle("POST /api/v1/builds", protect(roles.PermBuildsCreate, deps.BuildsH.Create))
		mux.Handle("GET /api/v1/builds", protectAny(deps.BuildsH.List, roles.PermBuildsReadSelf, roles.PermBuildsReadAny))
		mux.Handle("GET /api/v1/builds/{id}", protectAny(deps.BuildsH.Get, roles.PermBuildsReadSelf, roles.PermBuildsReadAny))
		mux.Handle("POST /api/v1/builds/{id}/cancel", protectAny(deps.BuildsH.Cancel, roles.PermBuildsCancelSelf, roles.PermBuildsCancelAny))
		mux.Handle("GET /api/v1/builds/{id}/logs", protectAny(deps.BuildsH.Logs, roles.PermBuildsReadSelf, roles.PermBuildsReadAny))
	}

	// MCP: the handler performs its own bearer authentication and RBAC
	// mapping internally, since its JSON-RPC error semantics differ from
	// REST's problem+json envelope.
	if deps.MCPHandler != nil {
		mux.Handle("/mcp", deps.MCPHandler)
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		WriteProblem(w, r, http.StatusNotFound, "not_found", "Not found", "")
	})

	chain := Chain(RequestID, Recover(deps.Logger), AccessLog(deps.Logger))
	return chain(mux)
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

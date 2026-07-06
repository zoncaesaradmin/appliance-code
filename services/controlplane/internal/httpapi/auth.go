package httpapi

import (
	"context"
	"errors"
	"net/http"

	"appliance-code/services/controlplane/internal/authz"
	"appliance-code/services/controlplane/internal/reqauth"
)

// Principal is an alias for reqauth.Principal so existing handler code
// written against httpapi.Principal keeps working unchanged now that
// resolution itself lives in the transport-neutral reqauth package.
type Principal = reqauth.Principal

// AuthDeps is an alias for reqauth.Deps.
type AuthDeps = reqauth.Deps

type principalCtxKey struct{}

// PrincipalFromContext returns the authenticated Principal stored on ctx by
// RequireAuth, if any.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalCtxKey{}).(Principal)
	return p, ok
}

// RequireAuth authenticates the request's Authorization bearer credential
// via reqauth.Authenticate and stores the resolved Principal on the request
// context.
func RequireAuth(deps AuthDeps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, _ := reqauth.BearerToken(r.Header.Get("Authorization"))
			principal, err := reqauth.Authenticate(r.Context(), deps, raw)
			if err != nil {
				if !errors.Is(err, reqauth.ErrUnauthenticated) && !errors.Is(err, reqauth.ErrInvalidCredential) {
					WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
					return
				}
				WriteProblem(w, r, http.StatusUnauthorized, "unauthorized", "Authentication required", "")
				return
			}

			ctx := context.WithValue(r.Context(), principalCtxKey{}, principal)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequirePermission denies the request with 403 unless the authenticated
// Principal (stored by RequireAuth, which must run first) holds permission.
func RequirePermission(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := PrincipalFromContext(r.Context())
			if !ok {
				WriteProblem(w, r, http.StatusUnauthorized, "unauthorized", "Authentication required", "")
				return
			}
			if !authz.HasPermission(principal.Permissions, permission) {
				WriteProblem(w, r, http.StatusForbidden, "forbidden", "Insufficient permissions", "")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAnyPermission denies the request with 403 unless the Principal
// holds at least one of permissions, for ".self"/".any" ownership pairs
// where the handler itself performs the finer-grained ownership check.
func RequireAnyPermission(permissions ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := PrincipalFromContext(r.Context())
			if !ok {
				WriteProblem(w, r, http.StatusUnauthorized, "unauthorized", "Authentication required", "")
				return
			}
			for _, perm := range permissions {
				if authz.HasPermission(principal.Permissions, perm) {
					next.ServeHTTP(w, r)
					return
				}
			}
			WriteProblem(w, r, http.StatusForbidden, "forbidden", "Insufficient permissions", "")
		})
	}
}

// Package forwardauth defines the stable request-to-permission mapping and
// trusted response-header contract for Traefik ForwardAuth checks. Keeping
// this mapping separate from the HTTP handler makes Phase 2's move to a
// dedicated auth service a deployment change rather than a policy rewrite.
package forwardauth

import (
	"net/http"
	"net/url"
	"strings"

	"appliance-code/services/controlplane/internal/appliance"
	"appliance-code/services/controlplane/internal/roles"
)

const (
	HeaderUserID     = "X-Appliance-User-Id"
	HeaderUsername   = "X-Appliance-Username"
	HeaderScopes     = "X-Appliance-Scopes"
	HeaderRoles      = "X-Appliance-Roles"
	HeaderAuthMethod = "X-Appliance-Auth-Method"
)

// Decision describes the authorization requirement for one forwarded request.
type Decision struct {
	Allowed    bool
	Capability appliance.Capability
	Permission string
	ReasonCode string
}

// RequiredPermission determines the current phase-1 application-level
// permission gate for a forwarded request using the original host, method,
// and URI Traefik provides. Unknown routes fail closed.
func RequiredPermission(host, method, rawURI string) Decision {
	_ = host // host-based routing is reserved for future application-specific rules.

	normalizedMethod := strings.ToUpper(strings.TrimSpace(method))
	path := normalizedPath(rawURI)

	switch {
	case path == "/mcp" || strings.HasPrefix(path, "/mcp/"):
		return Decision{Allowed: true, Capability: appliance.CapabilityBase, Permission: roles.PermMCPInvoke}
	case path == "/v2/" || path == "/v2" || strings.HasPrefix(path, "/v2/"):
		switch normalizedMethod {
		case http.MethodGet, http.MethodHead:
			return Decision{Allowed: true, Capability: appliance.CapabilityArtifact, Permission: roles.PermRegistryPull}
		case http.MethodPost, http.MethodPut, http.MethodPatch:
			return Decision{Allowed: true, Capability: appliance.CapabilityArtifact, Permission: roles.PermRegistryPush}
		case http.MethodDelete:
			return Decision{Allowed: true, Capability: appliance.CapabilityArtifact, Permission: roles.PermRegistryDelete}
		default:
			return Decision{Allowed: false, ReasonCode: "unsupported_method"}
		}
	default:
		return Decision{Allowed: false, ReasonCode: "no_matching_policy"}
	}
}

func normalizedPath(rawURI string) string {
	if rawURI == "" {
		return "/"
	}
	if strings.HasPrefix(rawURI, "/") {
		if u, err := url.ParseRequestURI(rawURI); err == nil && u.Path != "" {
			return u.Path
		}
		return rawURI
	}
	if u, err := url.Parse(rawURI); err == nil && u.Path != "" {
		return u.Path
	}
	return rawURI
}

package httpapi

import (
	"errors"
	"net/http"
	"sort"
	"strings"

	"appliance-code/services/controlplane/internal/appliance"
	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/authz"
	"appliance-code/services/controlplane/internal/forwardauth"
	"appliance-code/services/controlplane/internal/reqauth"
	"appliance-code/services/controlplane/internal/storage"
)

// ForwardAuthHandlers serves the internal-only Traefik ForwardAuth endpoint.
// It never proxies application traffic; it only authenticates, authorizes,
// emits trusted identity headers, and audits the decision.
type ForwardAuthHandlers struct {
	Auth         AuthDeps
	Audit        *audit.Recorder
	Capabilities appliance.Set
}

func (h *ForwardAuthHandlers) Check(w http.ResponseWriter, r *http.Request) {
	raw, _ := reqauth.BearerToken(r.Header.Get("Authorization"))
	principal, err := reqauth.Authenticate(r.Context(), h.Auth, raw)
	if err != nil {
		if errors.Is(err, reqauth.ErrUnauthenticated) || errors.Is(err, reqauth.ErrInvalidCredential) {
			h.recordAnonymous(r, storage.AuditOutcomeFailure, "unauthenticated", nil)
			WriteProblem(w, r, http.StatusUnauthorized, "unauthorized", "Authentication required", "")
			return
		}
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}

	host := firstHeader(r.Header, "X-Forwarded-Host", "X-Forwarded-Server")
	method := firstHeader(r.Header, "X-Forwarded-Method")
	uri := firstHeader(r.Header, "X-Forwarded-Uri")

	decision := forwardauth.RequiredPermission(host, method, uri)
	if !decision.Allowed {
		h.recordPrincipal(r, principal, storage.AuditOutcomeFailure, decision.ReasonCode, host, method, uri, "")
		WriteProblem(w, r, http.StatusForbidden, "forbidden", "Insufficient permissions", "")
		return
	}
	if !h.Capabilities.Enabled(decision.Capability) {
		h.recordPrincipal(r, principal, storage.AuditOutcomeFailure, "capability_disabled", host, method, uri, "")
		WriteProblem(w, r, http.StatusNotFound, "not_found", "Not found", "")
		return
	}
	if !authz.HasPermission(principal.Permissions, decision.Permission) {
		h.recordPrincipal(r, principal, storage.AuditOutcomeFailure, "insufficient_permission", host, method, uri, decision.Permission)
		WriteProblem(w, r, http.StatusForbidden, "forbidden", "Insufficient permissions", "")
		return
	}

	h.recordPrincipal(r, principal, storage.AuditOutcomeSuccess, "allowed", host, method, uri, decision.Permission)
	w.Header().Set(forwardauth.HeaderUserID, principal.UserID)
	w.Header().Set(forwardauth.HeaderUsername, principal.Username)
	w.Header().Set(forwardauth.HeaderAuthMethod, principal.AuthMethod)
	w.Header().Set(forwardauth.HeaderScopes, strings.Join(sortedPermissions(principal.Permissions), ","))
	w.Header().Set(forwardauth.HeaderRoles, strings.Join(sortedStrings(principal.RoleNames), ","))
	w.WriteHeader(http.StatusOK)
}

func (h *ForwardAuthHandlers) recordAnonymous(r *http.Request, outcome storage.AuditOutcome, reasonCode string, details map[string]any) {
	if h == nil || h.Audit == nil {
		return
	}
	actor := audit.Actor{
		Type:       storage.AuditActorAnonymous,
		AuthMethod: "forward_auth",
		RequestID:  requestIDFromRequest(r),
		SourceAddr: r.RemoteAddr,
	}
	_ = h.Audit.Record(r.Context(), actor, audit.Event{
		Action: "forward_auth.check", TargetType: "route", Outcome: outcome, ReasonCode: reasonCode,
		Details: details,
	})
}

func (h *ForwardAuthHandlers) recordPrincipal(r *http.Request, principal Principal, outcome storage.AuditOutcome, reasonCode, host, method, uri, permission string) {
	if h == nil || h.Audit == nil {
		return
	}
	details := map[string]any{
		"forwardedHost":   host,
		"forwardedMethod": method,
		"forwardedURI":    uri,
	}
	if permission != "" {
		details["permission"] = permission
	}
	_ = h.Audit.Record(r.Context(), principal.Actor(requestIDFromRequest(r), r.RemoteAddr), audit.Event{
		Action: "forward_auth.check", TargetType: "route", Outcome: outcome, ReasonCode: reasonCode,
		Details: details,
	})
}

func firstHeader(h http.Header, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(h.Get(name)); value != "" {
			return value
		}
	}
	return ""
}

func sortedPermissions(perms map[string]bool) []string {
	names := make([]string, 0, len(perms))
	for name, allowed := range perms {
		if allowed {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func sortedStrings(values []string) []string {
	cloned := append([]string(nil), values...)
	sort.Strings(cloned)
	return cloned
}

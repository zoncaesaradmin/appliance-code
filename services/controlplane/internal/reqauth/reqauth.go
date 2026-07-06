// Package reqauth resolves the plan's one authentication model — an
// opaque API token or a session access JWT presented as a bearer
// credential — into a Principal, identically for every transport. REST
// (internal/httpapi) and MCP (internal/mcp) both call Authenticate so the
// same credential always resolves to the same identity and permission set,
// per the plan's "consistent RBAC everywhere" requirement.
package reqauth

import (
	"context"
	"errors"
	"strings"

	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/authn"
	"appliance-code/services/controlplane/internal/authz"
	"appliance-code/services/controlplane/internal/storage"
	"appliance-code/services/controlplane/internal/tokens"
)

// Principal is the authenticated identity and resolved permission set for
// one request.
type Principal struct {
	UserID      string
	Permissions map[string]bool
	AuthMethod  string // "session" | "api_token"
	FamilyID    string // set for AuthMethod == "session"
	TokenID     string // set for AuthMethod == "api_token"
}

// Actor builds the audit.Actor for this principal, for handlers that need
// to record a security-relevant mutation.
func (p Principal) Actor(requestID, sourceAddr string) audit.Actor {
	credentialID := p.FamilyID
	if p.AuthMethod == "api_token" {
		credentialID = p.TokenID
	}
	return audit.Actor{
		UserID: p.UserID, Type: storage.AuditActorUser, AuthMethod: p.AuthMethod,
		CredentialID: credentialID, RequestID: requestID, SourceAddr: sourceAddr,
	}
}

// ErrUnauthenticated is returned when no usable bearer credential is
// present. ErrInvalidCredential is returned when one is present but does
// not authenticate. Callers map both to their transport's "unauthenticated"
// response; the distinction exists only so callers never need to inspect
// error text.
var (
	ErrUnauthenticated   = errors.New("reqauth: no credential presented")
	ErrInvalidCredential = errors.New("reqauth: invalid or expired credential")
)

// Deps bundles the services Authenticate needs to resolve either credential
// type in the plan's v1 identity model.
type Deps struct {
	Sessions *authn.SessionService
	Tokens   *tokens.Service
	Authz    *authz.Service
}

// BearerToken extracts the bearer credential from an Authorization header
// value (e.g. r.Header.Get("Authorization")), the only location the plan
// allows a credential to be presented from.
func BearerToken(authorizationHeader string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(authorizationHeader, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(authorizationHeader, prefix))
	if token == "" {
		return "", false
	}
	return token, true
}

// Authenticate resolves raw (as extracted by BearerToken) into a Principal,
// dispatching on its format: an opaque API token (apt_...) or a session
// access JWT.
func Authenticate(ctx context.Context, deps Deps, raw string) (Principal, error) {
	if raw == "" {
		return Principal{}, ErrUnauthenticated
	}

	if strings.HasPrefix(raw, authn.APITokenPrefix) {
		tok, err := deps.Tokens.Authenticate(ctx, raw)
		if err != nil {
			return Principal{}, ErrInvalidCredential
		}
		effective, err := deps.Authz.EffectivePermissions(ctx, tok.UserID)
		if err != nil {
			return Principal{}, err
		}
		return Principal{
			UserID: tok.UserID, Permissions: authz.IntersectScopes(effective, tok.Scopes),
			AuthMethod: "api_token", TokenID: tok.ID,
		}, nil
	}

	user, claims, err := deps.Sessions.ValidateAccessToken(ctx, raw)
	if err != nil {
		return Principal{}, ErrInvalidCredential
	}
	effective, err := deps.Authz.EffectivePermissions(ctx, user.ID)
	if err != nil {
		return Principal{}, err
	}
	return Principal{UserID: user.ID, Permissions: effective, AuthMethod: "session", FamilyID: claims.FamilyID}, nil
}

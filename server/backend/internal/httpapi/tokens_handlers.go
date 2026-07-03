package httpapi

import (
	"errors"
	"net/http"
	"time"

	"appliance-code/server/backend/internal/authz"
	"appliance-code/server/backend/internal/roles"
	"appliance-code/server/backend/internal/storage"
	"appliance-code/server/backend/internal/tokens"
)

// TokenHandlers implements the API Token Management HTTP surface.
type TokenHandlers struct {
	Tokens *tokens.Service
}

type tokenResponse struct {
	ID         string     `json:"id"`
	UserID     string     `json:"userId"`
	Name       string     `json:"name"`
	Scopes     []string   `json:"scopes,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
}

func toTokenResponse(t storage.APIToken) tokenResponse {
	return tokenResponse{
		ID: t.ID, UserID: t.UserID, Name: t.Name, Scopes: t.Scopes,
		CreatedAt: t.CreatedAt, ExpiresAt: t.ExpiresAt, LastUsedAt: t.LastUsedAt, RevokedAt: t.RevokedAt,
	}
}

type createTokenRequest struct {
	Name            string   `json:"name"`
	LifetimeSeconds int64    `json:"lifetimeSeconds"`
	Scopes          []string `json:"scopes"`
}

type createTokenResponse struct {
	Token string `json:"token"`
	tokenResponse
}

func (h *TokenHandlers) createFor(w http.ResponseWriter, r *http.Request, ownerUserID string) {
	var req createTokenRequest
	if err := decodeJSON(w, r, &req); err != nil || req.Name == "" {
		WriteValidationProblem(w, r, "name is required", nil)
		return
	}

	var ttl time.Duration
	if req.LifetimeSeconds > 0 {
		ttl = time.Duration(req.LifetimeSeconds) * time.Second
	}

	principal, _ := PrincipalFromContext(r.Context())
	raw, tok, err := h.Tokens.Create(r.Context(), principal.Actor(requestIDFromRequest(r), r.RemoteAddr), ownerUserID, req.Name, ttl, req.Scopes)
	if errors.Is(err, tokens.ErrInvalidLifetime) {
		WriteValidationProblem(w, r, err.Error(), nil)
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	writeJSON(w, http.StatusCreated, createTokenResponse{Token: raw, tokenResponse: toTokenResponse(tok)})
}

// CreateSelf handles POST /api/v1/tokens: the caller creates a token for
// themself.
func (h *TokenHandlers) CreateSelf(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	h.createFor(w, r, principal.UserID)
}

// CreateForUser handles POST /api/v1/users/{userId}/tokens: an
// administrator creates a token owned by another user.
func (h *TokenHandlers) CreateForUser(w http.ResponseWriter, r *http.Request) {
	h.createFor(w, r, r.PathValue("userId"))
}

// ListSelf handles GET /api/v1/tokens: the caller's own tokens.
func (h *TokenHandlers) ListSelf(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	h.listFor(w, r, principal.UserID)
}

func (h *TokenHandlers) listFor(w http.ResponseWriter, r *http.Request, userID string) {
	list, err := h.Tokens.ListByUser(r.Context(), userID)
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	items := make([]tokenResponse, len(list))
	for i, t := range list {
		items[i] = toTokenResponse(t)
	}
	writeJSON(w, http.StatusOK, struct {
		Items []tokenResponse `json:"items"`
	}{Items: items})
}

// RevokeSelf handles DELETE /api/v1/tokens/{id}, allowing revocation of any
// token the caller owns, or any token at all if they hold tokens.revoke.any.
func (h *TokenHandlers) RevokeSelf(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	id := r.PathValue("id")

	tok, err := h.Tokens.Get(r.Context(), id)
	if errors.Is(err, storage.ErrNotFound) {
		WriteProblem(w, r, http.StatusNotFound, "not_found", "Token not found", "")
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	if tok.UserID != principal.UserID && !authz.HasPermission(principal.Permissions, roles.PermTokensRevokeAny) {
		WriteProblem(w, r, http.StatusForbidden, "forbidden", "Insufficient permissions", "")
		return
	}
	if tok.RevokedAt != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if err := h.Tokens.Revoke(r.Context(), principal.Actor(requestIDFromRequest(r), r.RemoteAddr), id); err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RevokeForUser handles DELETE /api/v1/users/{userId}/tokens/{tokenId},
// requiring tokens.revoke.any at the route level and additionally
// confirming the token belongs to the named user.
func (h *TokenHandlers) RevokeForUser(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	userID, tokenID := r.PathValue("userId"), r.PathValue("tokenId")

	tok, err := h.Tokens.Get(r.Context(), tokenID)
	if errors.Is(err, storage.ErrNotFound) || (err == nil && tok.UserID != userID) {
		WriteProblem(w, r, http.StatusNotFound, "not_found", "Token not found", "")
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	if tok.RevokedAt != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if err := h.Tokens.Revoke(r.Context(), principal.Actor(requestIDFromRequest(r), r.RemoteAddr), tokenID); err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

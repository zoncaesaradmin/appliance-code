package httpapi

import (
	"errors"
	"net/http"
	"time"

	"appliance-code/services/controlplane/internal/authn"
)

// AuthHandlers implements POST /api/v1/auth/login, logout, refresh, and
// GET /api/v1/auth/session.
type AuthHandlers struct {
	Sessions *authn.SessionService
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	AccessToken     string    `json:"accessToken"`
	RefreshToken    string    `json:"refreshToken"`
	AccessExpiresAt time.Time `json:"accessExpiresAt"`
}

func (h *AuthHandlers) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(w, r, &req); err != nil {
		WriteValidationProblem(w, r, "invalid request body", nil)
		return
	}
	if req.Username == "" || req.Password == "" {
		WriteValidationProblem(w, r, "username and password are required", nil)
		return
	}

	result, err := h.Sessions.Login(r.Context(), r.RemoteAddr, requestIDFromRequest(r), req.Username, req.Password)
	switch {
	case errors.Is(err, authn.ErrAccountLocked):
		WriteProblem(w, r, http.StatusLocked, "account_locked", "Account temporarily locked", "")
		return
	case errors.Is(err, authn.ErrInvalidCredentials):
		WriteProblem(w, r, http.StatusUnauthorized, "invalid_credentials", "Invalid username or password", "")
		return
	case err != nil:
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}

	writeJSON(w, http.StatusOK, loginResponse{
		AccessToken: result.AccessToken, RefreshToken: result.RefreshToken, AccessExpiresAt: result.AccessExpiresAt,
	})
}

type refreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

func (h *AuthHandlers) Refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := decodeJSON(w, r, &req); err != nil || req.RefreshToken == "" {
		WriteValidationProblem(w, r, "refreshToken is required", nil)
		return
	}

	result, err := h.Sessions.Refresh(r.Context(), r.RemoteAddr, requestIDFromRequest(r), req.RefreshToken)
	if errors.Is(err, authn.ErrInvalidRefreshToken) {
		WriteProblem(w, r, http.StatusUnauthorized, "invalid_refresh_token", "Invalid or expired refresh token", "")
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}

	writeJSON(w, http.StatusOK, loginResponse{
		AccessToken: result.AccessToken, RefreshToken: result.RefreshToken, AccessExpiresAt: result.AccessExpiresAt,
	})
}

func (h *AuthHandlers) Logout(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok || principal.AuthMethod != "session" {
		WriteProblem(w, r, http.StatusBadRequest, "not_a_session", "Logout requires an interactive session credential", "")
		return
	}

	if err := h.Sessions.Logout(r.Context(), principal.Actor(requestIDFromRequest(r), r.RemoteAddr), principal.FamilyID); err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type sessionResponse struct {
	UserID      string   `json:"userId"`
	AuthMethod  string   `json:"authMethod"`
	Permissions []string `json:"permissions"`
}

func (h *AuthHandlers) Session(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		WriteProblem(w, r, http.StatusUnauthorized, "unauthorized", "Authentication required", "")
		return
	}

	perms := make([]string, 0, len(principal.Permissions))
	for name := range principal.Permissions {
		perms = append(perms, name)
	}
	writeJSON(w, http.StatusOK, sessionResponse{UserID: principal.UserID, AuthMethod: principal.AuthMethod, Permissions: perms})
}

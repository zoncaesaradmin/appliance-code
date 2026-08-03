package httpapi

import (
	"errors"
	"net/http"
	"time"

	"appliance-code/services/controlplane/internal/authn"
	"appliance-code/services/controlplane/internal/users"
)

// AuthHandlers implements POST /api/v1/auth/login, logout, refresh,
// change-password, and GET /api/v1/auth/session.
type AuthHandlers struct {
	Sessions *authn.SessionService
	Users    *users.Service
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Domain   string `json:"domain"`
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

	domain, err := authn.NormalizeAuthDomain(req.Domain)
	if err != nil {
		WriteValidationProblem(w, r, err.Error(), nil)
		return
	}

	result, err := h.Sessions.Login(r.Context(), r.RemoteAddr, requestIDFromRequest(r), domain, req.Username, req.Password)
	switch {
	case errors.Is(err, authn.ErrUnsupportedAuthDomain):
		WriteValidationProblem(w, r, err.Error(), nil)
		return
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

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

func (h *AuthHandlers) ChangePassword(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok || principal.AuthMethod != "session" {
		WriteProblem(w, r, http.StatusBadRequest, "not_a_session", "Changing password requires an interactive session credential", "")
		return
	}
	if h.Users == nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}

	var req changePasswordRequest
	if err := decodeJSON(w, r, &req); err != nil {
		WriteValidationProblem(w, r, "invalid request body", nil)
		return
	}
	if req.CurrentPassword == "" || req.NewPassword == "" {
		WriteValidationProblem(w, r, "currentPassword and newPassword are required", nil)
		return
	}

	err := h.Users.ChangePassword(
		r.Context(),
		principal.Actor(requestIDFromRequest(r), r.RemoteAddr),
		principal.UserID,
		req.CurrentPassword,
		req.NewPassword,
	)
	switch {
	case errors.Is(err, authn.ErrInvalidCredentials):
		WriteProblem(w, r, http.StatusUnauthorized, "invalid_credentials", "Current password is incorrect", "")
		return
	case err != nil:
		WriteValidationProblem(w, r, err.Error(), nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type sessionResponse struct {
	UserID      string   `json:"userId"`
	Username    string   `json:"username"`
	Domain      string   `json:"domain"`
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
	domain := principal.Domain
	if domain == "" {
		domain = authn.AuthDomainLocal
	}
	writeJSON(w, http.StatusOK, sessionResponse{
		UserID: principal.UserID, Username: principal.Username, Domain: domain,
		AuthMethod: principal.AuthMethod, Permissions: perms,
	})
}

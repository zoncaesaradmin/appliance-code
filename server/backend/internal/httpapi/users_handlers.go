package httpapi

import (
	"errors"
	"net/http"
	"time"

	"appliance-code/server/backend/internal/roles"
	"appliance-code/server/backend/internal/storage"
	"appliance-code/server/backend/internal/users"
)

// UserHandlers implements the User Management HTTP surface.
type UserHandlers struct {
	Users *users.Service
	Roles *roles.Service
}

type userResponse struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"displayName"`
	State       string    `json:"state"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func toUserResponse(u storage.User) userResponse {
	return userResponse{
		ID: u.ID, Username: u.Username, DisplayName: u.DisplayName, State: string(u.State),
		CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt,
	}
}

type createUserRequest struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Password    string `json:"password"`
}

func (h *UserHandlers) Create(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := decodeJSON(w, r, &req); err != nil {
		WriteValidationProblem(w, r, "invalid request body", nil)
		return
	}

	principal, _ := PrincipalFromContext(r.Context())
	user, err := h.Users.Create(r.Context(), principal.Actor(requestIDFromRequest(r), r.RemoteAddr), req.Username, req.DisplayName, req.Password)
	if err != nil {
		WriteValidationProblem(w, r, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusCreated, toUserResponse(user))
}

func (h *UserHandlers) List(w http.ResponseWriter, r *http.Request) {
	list, err := h.Users.List(r.Context())
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	items := make([]userResponse, len(list))
	for i, u := range list {
		items[i] = toUserResponse(u)
	}
	writeJSON(w, http.StatusOK, struct {
		Items []userResponse `json:"items"`
	}{Items: items})
}

func (h *UserHandlers) Get(w http.ResponseWriter, r *http.Request) {
	user, err := h.Users.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, storage.ErrNotFound) {
		WriteProblem(w, r, http.StatusNotFound, "not_found", "User not found", "")
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	writeJSON(w, http.StatusOK, toUserResponse(user))
}

type patchUserRequest struct {
	DisplayName *string `json:"displayName"`
}

func (h *UserHandlers) Patch(w http.ResponseWriter, r *http.Request) {
	var req patchUserRequest
	if err := decodeJSON(w, r, &req); err != nil {
		WriteValidationProblem(w, r, "invalid request body", nil)
		return
	}
	if req.DisplayName == nil {
		WriteValidationProblem(w, r, "displayName is the only mutable field", nil)
		return
	}

	principal, _ := PrincipalFromContext(r.Context())
	id := r.PathValue("id")
	if err := h.Users.UpdateDisplayName(r.Context(), principal.Actor(requestIDFromRequest(r), r.RemoteAddr), id, *req.DisplayName); err != nil {
		writeUserMutationError(w, r, err)
		return
	}
	user, err := h.Users.Get(r.Context(), id)
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	writeJSON(w, http.StatusOK, toUserResponse(user))
}

func (h *UserHandlers) Disable(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	err := h.Users.Disable(r.Context(), principal.Actor(requestIDFromRequest(r), r.RemoteAddr), r.PathValue("id"))
	if errors.Is(err, users.ErrLastAdministrator) {
		WriteProblem(w, r, http.StatusConflict, "last_administrator", "Cannot disable the last enabled administrator", "")
		return
	}
	if err != nil {
		writeUserMutationError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *UserHandlers) Enable(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	if err := h.Users.Enable(r.Context(), principal.Actor(requestIDFromRequest(r), r.RemoteAddr), r.PathValue("id")); err != nil {
		writeUserMutationError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type passwordResetResponse struct {
	ResetCredential string `json:"resetCredential"`
}

func (h *UserHandlers) PasswordReset(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	raw, err := h.Users.InitiatePasswordReset(r.Context(), principal.Actor(requestIDFromRequest(r), r.RemoteAddr), r.PathValue("id"))
	if err != nil {
		writeUserMutationError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, passwordResetResponse{ResetCredential: raw})
}

func (h *UserHandlers) Unlock(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	user, err := h.Users.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, storage.ErrNotFound) {
		WriteProblem(w, r, http.StatusNotFound, "not_found", "User not found", "")
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	if err := h.Users.Unlock(r.Context(), principal.Actor(requestIDFromRequest(r), r.RemoteAddr), user.Username); err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type setUserRolesRequest struct {
	RoleIDs []string `json:"roleIds"`
}

func (h *UserHandlers) SetRoles(w http.ResponseWriter, r *http.Request) {
	var req setUserRolesRequest
	if err := decodeJSON(w, r, &req); err != nil {
		WriteValidationProblem(w, r, "invalid request body", nil)
		return
	}
	principal, _ := PrincipalFromContext(r.Context())
	err := h.Roles.SetUserRoles(r.Context(), principal.Actor(requestIDFromRequest(r), r.RemoteAddr), r.PathValue("id"), req.RoleIDs)
	if errors.Is(err, roles.ErrLastAdministrator) {
		WriteProblem(w, r, http.StatusConflict, "last_administrator", "Cannot remove the last enabled administrator", "")
		return
	}
	if err != nil {
		writeUserMutationError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeUserMutationError maps the known sentinel errors these handlers can
// see to their HTTP status; anything else is an unexpected internal
// failure rather than a validation error, since these mutation paths don't
// return plain validation errors the way Create does.
func writeUserMutationError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, storage.ErrNotFound):
		WriteProblem(w, r, http.StatusNotFound, "not_found", "User not found", "")
	case errors.Is(err, users.ErrInvalidResetCredential):
		WriteProblem(w, r, http.StatusBadRequest, "invalid_reset_credential", err.Error(), "")
	default:
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
	}
}

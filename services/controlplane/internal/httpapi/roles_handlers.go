package httpapi

import (
	"errors"
	"net/http"

	"appliance-code/services/controlplane/internal/roles"
	"appliance-code/services/controlplane/internal/storage"
)

// RoleHandlers implements the Role Management HTTP surface.
type RoleHandlers struct {
	Roles *roles.Service
}

type roleResponse struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BuiltIn bool   `json:"builtIn"`
}

type permissionResponse struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func toRoleResponse(r storage.Role) roleResponse {
	return roleResponse{ID: r.ID, Name: r.Name, BuiltIn: r.BuiltIn}
}

func (h *RoleHandlers) ListPermissions(w http.ResponseWriter, r *http.Request) {
	perms, err := h.Roles.ListPermissions(r.Context())
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	items := make([]permissionResponse, len(perms))
	for i, p := range perms {
		items[i] = permissionResponse{Name: p.Name, Description: p.Description}
	}
	writeJSON(w, http.StatusOK, struct {
		Items []permissionResponse `json:"items"`
	}{Items: items})
}

func (h *RoleHandlers) List(w http.ResponseWriter, r *http.Request) {
	list, err := h.Roles.List(r.Context())
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	items := make([]roleResponse, len(list))
	for i, role := range list {
		items[i] = toRoleResponse(role)
	}
	writeJSON(w, http.StatusOK, struct {
		Items []roleResponse `json:"items"`
	}{Items: items})
}

type createRoleRequest struct {
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

func (h *RoleHandlers) Create(w http.ResponseWriter, r *http.Request) {
	var req createRoleRequest
	if err := decodeJSON(w, r, &req); err != nil || req.Name == "" {
		WriteValidationProblem(w, r, "name is required", nil)
		return
	}
	principal, _ := PrincipalFromContext(r.Context())
	role, err := h.Roles.Create(r.Context(), principal.Actor(requestIDFromRequest(r), r.RemoteAddr), req.Name, req.Permissions)
	if errors.Is(err, roles.ErrUnknownPermission) {
		WriteValidationProblem(w, r, err.Error(), nil)
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	writeJSON(w, http.StatusCreated, toRoleResponse(role))
}

type updateRoleRequest struct {
	Permissions []string `json:"permissions"`
}

func (h *RoleHandlers) Update(w http.ResponseWriter, r *http.Request) {
	var req updateRoleRequest
	if err := decodeJSON(w, r, &req); err != nil {
		WriteValidationProblem(w, r, "invalid request body", nil)
		return
	}
	principal, _ := PrincipalFromContext(r.Context())
	err := h.Roles.Update(r.Context(), principal.Actor(requestIDFromRequest(r), r.RemoteAddr), r.PathValue("id"), req.Permissions)
	writeRoleMutationError(w, r, err, func() { w.WriteHeader(http.StatusNoContent) })
}

func (h *RoleHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	err := h.Roles.Delete(r.Context(), principal.Actor(requestIDFromRequest(r), r.RemoteAddr), r.PathValue("id"))
	writeRoleMutationError(w, r, err, func() { w.WriteHeader(http.StatusNoContent) })
}

func writeRoleMutationError(w http.ResponseWriter, r *http.Request, err error, onSuccess func()) {
	switch {
	case err == nil:
		onSuccess()
	case errors.Is(err, storage.ErrNotFound):
		WriteProblem(w, r, http.StatusNotFound, "not_found", "Role not found", "")
	case errors.Is(err, roles.ErrBuiltInRole):
		WriteProblem(w, r, http.StatusConflict, "built_in_role", err.Error(), "")
	case errors.Is(err, roles.ErrUnknownPermission):
		WriteValidationProblem(w, r, err.Error(), nil)
	default:
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
	}
}

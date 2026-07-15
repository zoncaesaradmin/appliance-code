package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"appliance-code/services/controlplane/internal/authn"
	"appliance-code/services/controlplane/internal/bootstrap"
	"appliance-code/services/controlplane/internal/storage"
	"appliance-code/services/controlplane/internal/users"
)

type SetupHandlers struct {
	DB        storage.DB
	UserStore storage.UserStore
	RoleStore storage.RoleStore
	Users     *users.Service
}

type setupStatusResponse struct {
	Initialized bool `json:"initialized"`
}

type createFirstAdminRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"displayName"`
}

type createFirstAdminResponse struct {
	Username string `json:"username"`
}

func (h *SetupHandlers) Status(w http.ResponseWriter, r *http.Request) {
	initialized, err := bootstrap.Initialized(r.Context(), h.UserStore)
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	writeJSON(w, http.StatusOK, setupStatusResponse{Initialized: initialized})
}

func (h *SetupHandlers) CreateFirstAdmin(w http.ResponseWriter, r *http.Request) {
	var req createFirstAdminRequest
	if err := decodeJSON(w, r, &req); err != nil {
		WriteValidationProblem(w, r, "invalid request body", nil)
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	if req.Username == "" || req.Password == "" {
		WriteValidationProblem(w, r, "username and password are required", nil)
		return
	}
	if _, err := authn.NormalizeUsername(req.Username); err != nil {
		WriteValidationProblem(w, r, err.Error(), nil)
		return
	}
	if err := authn.ValidatePasswordPolicy(req.Password); err != nil {
		WriteValidationProblem(w, r, err.Error(), nil)
		return
	}
	if req.DisplayName == "" {
		req.DisplayName = req.Username
	}

	created, err := bootstrap.Init(r.Context(), h.DB, h.UserStore, h.RoleStore, h.Users, req.Username, req.Password, req.DisplayName)
	switch {
	case errors.Is(err, bootstrap.ErrAlreadyInitialized):
		WriteProblem(w, r, http.StatusConflict, "already_initialized", "Appliance is already initialized", "")
		return
	case err != nil:
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	writeJSON(w, http.StatusCreated, createFirstAdminResponse{Username: created.Username})
}

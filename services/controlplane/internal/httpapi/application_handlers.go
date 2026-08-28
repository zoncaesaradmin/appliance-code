package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"appliance-code/services/controlplane/internal/applications"
	"appliance-code/services/controlplane/internal/storage"
)

// ApplicationHandlers exposes the Application Management capability on the
// existing Control Plane listener. It does not proxy or invoke Automation
// Runtime.
type ApplicationHandlers struct {
	Applications *applications.Service
}

func (h *ApplicationHandlers) ListDefinitions(w http.ResponseWriter, r *http.Request) {
	items, err := h.Applications.ListDefinitions(r.Context())
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *ApplicationHandlers) RegisterDefinition(w http.ResponseWriter, r *http.Request) {
	var document json.RawMessage
	if err := decodeJSON(w, r, &document); err != nil {
		WriteProblem(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body", "")
		return
	}
	definition, err := h.Applications.Register(r.Context(), document)
	if errors.Is(err, applications.ErrInvalidDefinition) {
		WriteProblem(w, r, http.StatusBadRequest, "invalid_definition", err.Error(), "")
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	writeJSON(w, http.StatusCreated, definition)
}

func (h *ApplicationHandlers) GetDefinition(w http.ResponseWriter, r *http.Request) {
	definition, err := h.Applications.GetDefinition(r.Context(), r.PathValue("name"), r.URL.Query().Get("version"))
	if errors.Is(err, applications.ErrNotFound) || errors.Is(err, storage.ErrNotFound) {
		WriteProblem(w, r, http.StatusNotFound, "not_found", "Application definition not found", "")
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	writeJSON(w, http.StatusOK, definition)
}

func (h *ApplicationHandlers) ListInstances(w http.ResponseWriter, r *http.Request) {
	items, err := h.Applications.ListInstances(r.Context())
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type installApplicationRequest struct {
	Version string `json:"version"`
}

func (h *ApplicationHandlers) Install(w http.ResponseWriter, r *http.Request) {
	var req installApplicationRequest
	if err := decodeJSON(w, r, &req); err != nil {
		WriteProblem(w, r, http.StatusBadRequest, "invalid_json", "Invalid JSON body", "")
		return
	}
	instance, err := h.Applications.Install(r.Context(), r.PathValue("name"), req.Version)
	if errors.Is(err, applications.ErrNotFound) || errors.Is(err, storage.ErrNotFound) {
		WriteProblem(w, r, http.StatusNotFound, "not_found", "Application definition not found", "")
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	writeJSON(w, http.StatusAccepted, instance)
}

func (h *ApplicationHandlers) Disable(w http.ResponseWriter, r *http.Request) {
	instance, err := h.Applications.Disable(r.Context(), r.PathValue("name"))
	if errors.Is(err, applications.ErrNotFound) || errors.Is(err, storage.ErrNotFound) {
		WriteProblem(w, r, http.StatusNotFound, "not_found", "Application instance not found", "")
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	writeJSON(w, http.StatusAccepted, instance)
}

func (h *ApplicationHandlers) GetInstance(w http.ResponseWriter, r *http.Request) {
	instance, err := h.Applications.GetInstance(r.Context(), r.PathValue("name"))
	if errors.Is(err, applications.ErrNotFound) || errors.Is(err, storage.ErrNotFound) {
		WriteProblem(w, r, http.StatusNotFound, "not_found", "Application instance not found", "")
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	writeJSON(w, http.StatusOK, instance)
}

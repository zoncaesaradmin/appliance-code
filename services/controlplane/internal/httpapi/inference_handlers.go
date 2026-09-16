package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/inference"
	"appliance-code/services/controlplane/internal/storage"
)

type InferenceHandlers struct {
	Inference *inference.Service
	Audit     *audit.Recorder
}

func (h *InferenceHandlers) Capabilities(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.Inference.Capabilities(r.Context()))
}

func (h *InferenceHandlers) Status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.Inference.Status(r.Context()))
}

func (h *InferenceHandlers) Models(w http.ResponseWriter, r *http.Request) {
	models, err := h.Inference.ListModels(r.Context())
	if err != nil {
		WriteProblem(w, r, http.StatusBadGateway, "inference_unavailable", "The inference runtime is unavailable", "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": models})
}

func (h *InferenceHandlers) Catalog(w http.ResponseWriter, r *http.Request) {
	catalog, err := h.Inference.Catalog(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, catalog)
}

func (h *InferenceHandlers) Import(w http.ResponseWriter, r *http.Request) {
	var req inference.ImportRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	progress, err := h.Inference.Import(r.Context(), req)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.record(r, "inference.model.import", req.ModelID)
	writeJSON(w, http.StatusAccepted, progress)
}

func (h *InferenceHandlers) ImportProgress(w http.ResponseWriter, r *http.Request) {
	progress, err := h.Inference.ImportProgress(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, progress)
}

func (h *InferenceHandlers) Load(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ModelID string `json:"modelId"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	id := strings.TrimSpace(req.ModelID)
	if err := h.Inference.Load(r.Context(), id); err != nil {
		h.writeError(w, r, err)
		return
	}
	h.record(r, "inference.model.load", id)
	writeJSON(w, http.StatusAccepted, map[string]any{"modelId": id, "status": "loaded"})
}

func (h *InferenceHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ModelID string `json:"modelId"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	id := strings.TrimSpace(req.ModelID)
	if err := h.Inference.Delete(r.Context(), id); err != nil {
		h.writeError(w, r, err)
		return
	}
	h.record(r, "inference.model.delete", id)
	w.WriteHeader(http.StatusNoContent)
}

func (h *InferenceHandlers) writeError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, inference.ErrInvalidRequest) {
		WriteProblem(w, r, http.StatusBadRequest, "invalid_inference_request", err.Error(), "")
		return
	}
	if errors.Is(err, inference.ErrUnsupported) {
		WriteProblem(w, r, http.StatusUnprocessableEntity, "inference_operation_unsupported", err.Error(), "")
		return
	}
	if errors.Is(err, inference.ErrBusy) {
		WriteProblem(w, r, http.StatusConflict, "inference_operation_in_progress", err.Error(), "")
		return
	}
	WriteProblem(w, r, http.StatusBadGateway, "inference_operation_failed", "The inference runtime rejected the operation", "")
}

func (h *InferenceHandlers) record(r *http.Request, action, target string) {
	if h.Audit == nil {
		return
	}
	principal, _ := PrincipalFromContext(r.Context())
	_ = h.Audit.Record(r.Context(), principal.Actor(requestIDFromRequest(r), r.RemoteAddr), audit.Event{
		Action: action, TargetType: "inference_model", TargetID: target,
		Outcome: storage.AuditOutcomeSuccess,
		Details: map[string]any{"method": r.Method},
	})
}

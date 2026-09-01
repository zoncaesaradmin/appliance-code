package httpapi

import (
	"errors"
	"net/http"
	"path"
	"strings"
	"time"

	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/storage"
)

type FocusContentHandlers struct {
	Store storage.FocusContentStore
	Audit *audit.Recorder
}

type setFocusContentRequest struct {
	ResourcePath string `json:"resourcePath"`
	Title        string `json:"title"`
	Message      string `json:"message"`
}

func (h *FocusContentHandlers) Get(w http.ResponseWriter, r *http.Request) {
	content, err := h.Store.GetFocusContent(r.Context())
	if errors.Is(err, storage.ErrNotFound) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	writeJSON(w, http.StatusOK, content)
}

func (h *FocusContentHandlers) Put(w http.ResponseWriter, r *http.Request) {
	var req setFocusContentRequest
	if err := decodeJSON(w, r, &req); err != nil {
		WriteValidationProblem(w, r, "invalid request body", nil)
		return
	}
	resourcePath := strings.Trim(strings.TrimSpace(req.ResourcePath), "/")
	title := strings.TrimSpace(req.Title)
	message := strings.TrimSpace(req.Message)
	if resourcePath == "" || path.Clean(resourcePath) != resourcePath || strings.HasPrefix(resourcePath, "../") || !strings.HasSuffix(strings.ToLower(resourcePath), ".mp4") || title == "" {
		WriteValidationProblem(w, r, "resourcePath must be a video-library MP4 path and title is required", nil)
		return
	}
	actor, _ := PrincipalFromContext(r.Context())
	content := storage.FocusContent{ResourceType: "video", ResourcePath: resourcePath, Title: title, Message: message, PublishedAt: time.Now().UTC(), PublishedBy: actor.UserID}
	if err := h.Store.PutFocusContent(r.Context(), content); err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	h.record(r, "focus.content.publish", resourcePath)
	writeJSON(w, http.StatusOK, content)
}

func (h *FocusContentHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.Store.ClearFocusContent(r.Context()); err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	h.record(r, "focus.content.clear", "current")
	w.WriteHeader(http.StatusNoContent)
}

func (h *FocusContentHandlers) record(r *http.Request, action, target string) {
	if h.Audit == nil {
		return
	}
	actor, _ := PrincipalFromContext(r.Context())
	_ = h.Audit.Record(r.Context(), actor.Actor(requestIDFromRequest(r), r.RemoteAddr), audit.Event{Action: action, TargetType: "focus-content", TargetID: target, Outcome: storage.AuditOutcomeSuccess})
}

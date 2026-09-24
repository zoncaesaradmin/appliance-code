package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"appliance-code/services/controlplane/internal/chat"
	"appliance-code/services/controlplane/internal/storage"
)

// ChatHandlers exposes only the appliance-owned chat workflow; browsers do
// not choose models, reach the inference runtime directly, or receive another
// user's persisted history.
type ChatHandlers struct{ Chat *chat.Service }

func (h *ChatHandlers) Availability(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.Chat.Availability(r.Context()))
}
func (h *ChatHandlers) List(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFromContext(r.Context())
	items, err := h.Chat.List(r.Context(), p.UserID)
	if err != nil {
		h.error(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (h *ChatHandlers) Create(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFromContext(r.Context())
	c, err := h.Chat.CreateConversation(r.Context(), p.UserID)
	if err != nil {
		h.error(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}
func (h *ChatHandlers) Get(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFromContext(r.Context())
	c, messages, err := h.Chat.Get(r.Context(), p.UserID, r.PathValue("id"))
	if err != nil {
		h.error(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversation": c, "messages": messages})
}
func (h *ChatHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFromContext(r.Context())
	if err := h.Chat.Delete(r.Context(), p.UserID, r.PathValue("id")); err != nil {
		h.error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ChatHandlers) Turn(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Content string `json:"content"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	p, _ := PrincipalFromContext(r.Context())
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteProblem(w, r, http.StatusInternalServerError, "streaming_unsupported", "Streaming is unavailable", "")
		return
	}
	emit := func(event string, value any) error {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}
	if err := h.Chat.StreamTurn(r.Context(), p.UserID, r.PathValue("id"), request.Content, emit); err != nil {
		_ = emit("failed", map[string]string{"message": h.message(err)})
	}
}

func (h *ChatHandlers) error(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	code, title := "chat_failed", "Chat is unavailable"
	switch {
	case errors.Is(err, storage.ErrNotFound):
		status, code, title = http.StatusNotFound, "chat_not_found", "Conversation not found"
	case errors.Is(err, chat.ErrUnavailable):
		status, code, title = http.StatusConflict, "chat_unavailable", "No model is enabled"
	case errors.Is(err, chat.ErrModelChanged):
		status, code, title = http.StatusConflict, "chat_model_changed", "The enabled model changed"
	case errors.Is(err, chat.ErrTooMany):
		status, code, title = http.StatusConflict, "chat_limit_reached", "Conversation limit reached"
	case errors.Is(err, chat.ErrInvalidPrompt):
		status, code, title = http.StatusBadRequest, "invalid_chat_prompt", "Enter a shorter message"
	}
	WriteProblem(w, r, status, code, title, "")
}
func (h *ChatHandlers) message(err error) string {
	if errors.Is(err, chat.ErrUnavailable) {
		return "No model is enabled. Ask an administrator to enable one."
	}
	if errors.Is(err, chat.ErrModelChanged) {
		return "The enabled model changed. Start a new conversation."
	}
	if strings.TrimSpace(err.Error()) == "" {
		return "The response stopped before completion."
	}
	return "The response stopped before completion."
}

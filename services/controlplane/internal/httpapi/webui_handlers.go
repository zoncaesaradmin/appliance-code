package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"appliance-code/services/controlplane/internal/inference"
	"appliance-code/services/controlplane/internal/webui"
)

// WebUIHandlers deliberately expose only opaque handoff capabilities. Grant
// and bridge credentials are never written to audit/event details or logs.
type WebUIHandlers struct {
	WebUI     *webui.Service
	Inference *inference.Service
	Enabled   bool
}

func (h *WebUIHandlers) Launch(w http.ResponseWriter, r *http.Request) {
	if !h.Enabled {
		WriteProblem(w, r, http.StatusNotFound, "webui_unavailable", "Open AI Workspace is not installed", "")
		return
	}
	p, ok := PrincipalFromContext(r.Context())
	if !ok || p.AuthMethod != "session" {
		WriteProblem(w, r, http.StatusUnauthorized, "interactive_session_required", "An interactive session is required", "")
		return
	}
	status := h.Inference.Status(r.Context())
	if !status.Ready || status.ServingState != "ready" || strings.TrimSpace(status.LoadedModelID) == "" {
		WriteProblem(w, r, http.StatusConflict, "webui_model_unavailable", "Open AI Workspace requires a ready loaded model", "")
		return
	}
	grant, err := h.WebUI.Issue(r.Context(), p.UserID, p.FamilyID)
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "webui_launch_failed", "Could not create workspace launch", "")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, map[string]string{"grant": grant})
}

// Consume is called by the session bridge after a browser form POST. It is an
// opaque capability endpoint: a 256-bit single-use grant is required and no
// authenticated API token can invoke it. The gateway is the only chart route
// permitted to reach it by NetworkPolicy.
func (h *WebUIHandlers) Consume(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Grant string `json:"grant"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		WriteProblem(w, r, http.StatusBadRequest, "invalid_grant", "Invalid workspace launch", "")
		return
	}
	bridge, userID, displayName, err := h.WebUI.ConsumeGrant(r.Context(), strings.TrimSpace(body.Grant))
	if err != nil {
		WriteProblem(w, r, http.StatusUnauthorized, "invalid_grant", "Invalid or expired workspace launch", "")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{"bridge": bridge, "userId": userID, "displayName": displayName})
}

func (h *WebUIHandlers) Validate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Bridge string `json:"bridge"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		WriteProblem(w, r, http.StatusBadRequest, "invalid_bridge", "Invalid workspace session", "")
		return
	}
	userID, displayName, err := h.WebUI.ValidateBridge(r.Context(), strings.TrimSpace(body.Bridge))
	if err != nil {
		WriteProblem(w, r, http.StatusUnauthorized, "invalid_bridge", "Workspace session expired", "")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{"userId": userID, "displayName": displayName})
}

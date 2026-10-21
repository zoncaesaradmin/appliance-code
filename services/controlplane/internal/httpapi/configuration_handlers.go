package httpapi

import (
	"encoding/json"
	"net/http"

	"appliance-code/services/controlplane/internal/configuration"
	"appliance-code/services/controlplane/internal/storage"
)

// ConfigurationHandlers exposes the desired-state revision boundary. Archive
// encryption, target storage, and destructive import are intentionally not
// public HTTP operations yet; those require the coordinated recovery-set
// protocol and installer bootstrap state machine.
type ConfigurationHandlers struct {
	Configuration *configuration.Service
}

type configurationRevisionResponse struct {
	ID            string          `json:"id"`
	SchemaVersion string          `json:"schemaVersion"`
	Payload       json.RawMessage `json:"payload"`
	PayloadDigest string          `json:"payloadDigest"`
	CreatedAt     string          `json:"createdAt"`
	CreatedBy     string          `json:"createdBy,omitempty"`
	Status        string          `json:"status"`
	ApplyMessage  string          `json:"applyMessage,omitempty"`
}

func (h *ConfigurationHandlers) Capture(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	revision, err := h.Configuration.Capture(r.Context(), principal.UserID)
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "configuration_capture_failed", "Configuration capture failed", "")
		return
	}
	writeJSON(w, http.StatusCreated, configurationRevisionResponseFrom(revision))
}

func (h *ConfigurationHandlers) Latest(w http.ResponseWriter, r *http.Request) {
	revision, err := h.Configuration.Latest(r.Context())
	if err == storage.ErrNotFound {
		WriteProblem(w, r, http.StatusNotFound, "configuration_revision_not_found", "Configuration revision not found", "")
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "configuration_read_failed", "Configuration read failed", "")
		return
	}
	writeJSON(w, http.StatusOK, configurationRevisionResponseFrom(revision))
}

func configurationRevisionResponseFrom(revision storage.ConfigurationRevision) configurationRevisionResponse {
	return configurationRevisionResponse{
		ID: revision.ID, SchemaVersion: revision.SchemaVersion, Payload: json.RawMessage(revision.Payload),
		PayloadDigest: revision.PayloadDigest, CreatedAt: revision.CreatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
		CreatedBy: revision.CreatedBy, Status: revision.Status, ApplyMessage: revision.ApplyMessage,
	}
}

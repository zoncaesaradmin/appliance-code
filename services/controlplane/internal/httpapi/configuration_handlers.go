package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"appliance-code/services/controlplane/internal/configuration"
	"appliance-code/services/controlplane/internal/storage"
)

// ConfigurationHandlers exposes the desired-state revision boundary. Archive
// encryption and target storage are handled by the caller's explicit backup
// target/key contract; this API only accepts a validated, already-decrypted
// desired-state snapshot and refuses to merge it into existing state.
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

type configurationImportRequest struct {
	Payload json.RawMessage `json:"payload"`
}

// Import applies a portable desired-state payload only to a clean
// destination. It is intentionally a system-operator operation; zonctl uses
// it after it has staged, authenticated, decrypted, and compatibility-checked
// an archive. It is not a PostgreSQL recovery-set endpoint.
func (h *ConfigurationHandlers) Import(w http.ResponseWriter, r *http.Request) {
	var request configurationImportRequest
	if err := decodeJSON(w, r, &request); err != nil || len(request.Payload) == 0 {
		WriteProblem(w, r, http.StatusBadRequest, "configuration_import_invalid", "Configuration import payload is invalid", "")
		return
	}
	principal, _ := PrincipalFromContext(r.Context())
	revision, err := h.Configuration.Import(r.Context(), request.Payload, principal.UserID)
	if errors.Is(err, configuration.ErrInvalidSnapshot) {
		WriteProblem(w, r, http.StatusBadRequest, "configuration_import_invalid", "Configuration import payload is invalid", "")
		return
	}
	if errors.Is(err, configuration.ErrDestinationNotClean) {
		WriteProblem(w, r, http.StatusConflict, "configuration_import_requires_clean_target", "Configuration import requires a clean target", "")
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "configuration_import_failed", "Configuration import failed", "")
		return
	}
	writeJSON(w, http.StatusCreated, configurationRevisionResponseFrom(revision))
}

func configurationRevisionResponseFrom(revision storage.ConfigurationRevision) configurationRevisionResponse {
	return configurationRevisionResponse{
		ID: revision.ID, SchemaVersion: revision.SchemaVersion, Payload: json.RawMessage(revision.Payload),
		PayloadDigest: revision.PayloadDigest, CreatedAt: revision.CreatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
		CreatedBy: revision.CreatedBy, Status: revision.Status, ApplyMessage: revision.ApplyMessage,
	}
}

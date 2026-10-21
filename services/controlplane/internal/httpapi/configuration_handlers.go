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
	Archives      configuration.ArchiveService
}

type configurationArchiveRequest struct {
	RevisionID string `json:"revisionId,omitempty"`
}

// ListArchives returns only cryptographically verified archive manifests.
func (h *ConfigurationHandlers) ListArchives(w http.ResponseWriter, r *http.Request) {
	archives, err := h.Archives.ListVerified(r.Context())
	if err != nil {
		WriteProblem(w, r, http.StatusConflict, "configuration_archive_list_failed", "Configuration archive listing failed", "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"archives": archives})
}

// Archive captures the current desired configuration unless the caller
// selects an existing immutable revision, then seals it into the configured
// same-appliance archive target. It returns only non-secret manifest data.
func (h *ConfigurationHandlers) Archive(w http.ResponseWriter, r *http.Request) {
	var request configurationArchiveRequest
	if err := decodeJSON(w, r, &request); err != nil {
		WriteProblem(w, r, http.StatusBadRequest, "configuration_archive_invalid", "Configuration archive request is invalid", "")
		return
	}
	revisionID := request.RevisionID
	if revisionID == "" {
		principal, _ := PrincipalFromContext(r.Context())
		revision, err := h.Configuration.Capture(r.Context(), principal.UserID)
		if err != nil {
			WriteProblem(w, r, http.StatusInternalServerError, "configuration_capture_failed", "Configuration capture failed", "")
			return
		}
		revisionID = revision.ID
	}
	manifest, err := h.Archives.Export(r.Context(), revisionID)
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "configuration_archive_failed", "Configuration archive failed", "")
		return
	}
	writeJSON(w, http.StatusCreated, manifest)
}

// VerifyArchive authenticates and decrypts an archive without changing
// configuration. This is the required precondition for restore authorization.
func (h *ConfigurationHandlers) VerifyArchive(w http.ResponseWriter, r *http.Request) {
	manifest, err := h.Archives.Verify(r.Context(), r.PathValue("archiveID"))
	if errors.Is(err, configuration.ErrArchiveNotFound) {
		WriteProblem(w, r, http.StatusNotFound, "configuration_archive_not_found", "Configuration archive not found", "")
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusConflict, "configuration_archive_invalid", "Configuration archive verification failed", "")
		return
	}
	writeJSON(w, http.StatusOK, manifest)
}

// DownloadArchive returns a sealed archive only after authenticating it. The
// bytes are deliberately an attachment rather than JSON API data so ordinary
// API logging/redaction does not attempt to inspect or retain them.
func (h *ConfigurationHandlers) DownloadArchive(w http.ResponseWriter, r *http.Request) {
	manifest, data, err := h.Archives.ReadVerified(r.Context(), r.PathValue("archiveID"))
	if errors.Is(err, configuration.ErrArchiveNotFound) {
		WriteProblem(w, r, http.StatusNotFound, "configuration_archive_not_found", "Configuration archive not found", "")
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusConflict, "configuration_archive_invalid", "Configuration archive verification failed", "")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.zon.configuration-archive+json")
	w.Header().Set("Content-Disposition", `attachment; filename="`+manifest.ArchiveID+`.json"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// ImportArchive is limited to the encrypted archive target configured on this
// appliance. Cross-appliance imports use the portable archive/key handoff
// path and the plain Import endpoint only after zonctl has authenticated and
// decrypted that external archive.
func (h *ConfigurationHandlers) ImportArchive(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	revision, err := h.Archives.Import(r.Context(), r.PathValue("archiveID"), principal.UserID)
	if errors.Is(err, configuration.ErrArchiveNotFound) {
		WriteProblem(w, r, http.StatusNotFound, "configuration_archive_not_found", "Configuration archive not found", "")
		return
	}
	if errors.Is(err, configuration.ErrDestinationNotClean) || errors.Is(err, configuration.ErrIncompatibleSnapshot) {
		WriteProblem(w, r, http.StatusConflict, "configuration_archive_import_refused", "Configuration archive import was refused", "")
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusBadRequest, "configuration_archive_invalid", "Configuration archive import failed", "")
		return
	}
	writeJSON(w, http.StatusCreated, configurationRevisionResponseFrom(revision))
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
	if errors.Is(err, configuration.ErrIncompatibleSnapshot) {
		WriteProblem(w, r, http.StatusConflict, "configuration_import_incompatible", "Configuration import is incompatible with this target", "")
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

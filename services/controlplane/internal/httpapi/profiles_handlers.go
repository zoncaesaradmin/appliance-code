package httpapi

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/metadatabundle"
	"appliance-code/services/controlplane/internal/profiles"
	"appliance-code/services/controlplane/internal/storage"
)

type ProfileHandlers struct {
	Profiles *profiles.Service
}

func (h *ProfileHandlers) actor(r *http.Request) audit.Actor {
	principal, _ := PrincipalFromContext(r.Context())
	return audit.Actor{
		UserID:     principal.UserID,
		Type:       storage.AuditActorUser,
		AuthMethod: principal.AuthMethod,
	}
}

func (h *ProfileHandlers) ListCapabilities(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": h.Profiles.ListCapabilities()})
}

func (h *ProfileHandlers) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.Profiles.List(r.Context())
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	if items == nil {
		items = []profiles.ProfileView{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *ProfileHandlers) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("profileId")
	item, err := h.Profiles.Get(r.Context(), id)
	if errors.Is(err, profiles.ErrNotFound) {
		WriteProblem(w, r, http.StatusNotFound, "not_found", "Profile not found", "")
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *ProfileHandlers) Validate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("profileId")
	result, err := h.Profiles.Validate(r.Context(), id)
	if errors.Is(err, profiles.ErrNotFound) {
		WriteProblem(w, r, http.StatusNotFound, "not_found", "Profile not found", "")
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *ProfileHandlers) Activate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("profileId")
	result, validation, err := h.Profiles.Activate(r.Context(), h.actor(r), id)
	if errors.Is(err, profiles.ErrLicensingUnresolved) {
		WriteProblem(w, r, http.StatusConflict, "licensing_unresolved", "Licensing must be resolved before activating profiles", "")
		return
	}
	if errors.Is(err, profiles.ErrValidationFailed) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"code":       "activation_validation_failed",
			"message":    "Profile activation validation failed",
			"validation": validation,
		})
		return
	}
	if errors.Is(err, profiles.ErrNotFound) {
		WriteProblem(w, r, http.StatusNotFound, "not_found", "Profile not found", "")
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"activation": result,
		"validation": validation,
	})
}

type MetadataBundleHandlers struct {
	Metadata *metadatabundle.Service
}

func (h *MetadataBundleHandlers) actor(r *http.Request) audit.Actor {
	principal, _ := PrincipalFromContext(r.Context())
	return audit.Actor{
		UserID:     principal.UserID,
		Type:       storage.AuditActorUser,
		AuthMethod: principal.AuthMethod,
	}
}

func (h *MetadataBundleHandlers) Status(w http.ResponseWriter, r *http.Request) {
	st, err := h.Metadata.Status(r.Context())
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

type metadataArchiveRequest struct {
	ArchivePath string `json:"archivePath"`
	Signature   string `json:"signature"`
}

func (h *MetadataBundleHandlers) Validate(w http.ResponseWriter, r *http.Request) {
	path, signature, cleanup, err := h.readUploadedArchive(w, r)
	if err != nil {
		return
	}
	defer cleanup()
	validation, _, err := h.Metadata.ValidateArchive(r.Context(), path, signature)
	if errors.Is(err, metadatabundle.ErrInvalidSig) {
		WriteValidationProblem(w, r, err.Error(), nil)
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusBadRequest, "invalid_metadata_bundle", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, validation)
}

func (h *MetadataBundleHandlers) Install(w http.ResponseWriter, r *http.Request) {
	path, signature, cleanup, err := h.readUploadedArchive(w, r)
	if err != nil {
		return
	}
	defer cleanup()
	st, validation, err := h.Metadata.InstallArchive(r.Context(), h.actor(r), path, signature)
	if errors.Is(err, metadatabundle.ErrInvalidSig) {
		WriteValidationProblem(w, r, err.Error(), nil)
		return
	}
	if errors.Is(err, metadatabundle.ErrInvalidArchive) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"code":       "metadata_bundle_invalid",
			"message":    "Metadata bundle validation failed",
			"validation": validation,
		})
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": st, "validation": validation})
}

func (h *MetadataBundleHandlers) Rollback(w http.ResponseWriter, r *http.Request) {
	st, err := h.Metadata.Rollback(r.Context(), h.actor(r))
	if err != nil {
		WriteProblem(w, r, http.StatusConflict, "rollback_failed", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (h *MetadataBundleHandlers) readUploadedArchive(w http.ResponseWriter, r *http.Request) (string, string, func(), error) {
	cleanup := func() {}
	if err := r.ParseMultipartForm(64 << 20); err == nil {
		signature := r.FormValue("signature")
		if signature == "" {
			signature = "offline-dev"
		}
		file, hdr, err := r.FormFile("archive")
		if err != nil {
			WriteValidationProblem(w, r, "multipart field archive is required", nil)
			return "", "", cleanup, err
		}
		defer file.Close()
		tmp, err := os.CreateTemp("", "metadata-upload-*.tar.zst")
		if err != nil {
			WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
			return "", "", cleanup, err
		}
		if _, err := io.Copy(tmp, file); err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
			return "", "", cleanup, err
		}
		tmp.Close()
		cleanup = func() { os.Remove(tmp.Name()) }
		_ = hdr
		return tmp.Name(), signature, cleanup, nil
	}

	var req metadataArchiveRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return "", "", cleanup, err
	}
	if req.Signature == "" {
		req.Signature = "offline-dev"
	}
	if req.ArchivePath == "" {
		WriteValidationProblem(w, r, "archivePath is required for JSON install/validate", nil)
		return "", "", cleanup, errors.New("missing archive")
	}
	if !filepath.IsAbs(req.ArchivePath) {
		WriteValidationProblem(w, r, "archivePath must be absolute", nil)
		return "", "", cleanup, errors.New("relative path")
	}
	return req.ArchivePath, req.Signature, cleanup, nil
}

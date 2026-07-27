package httpapi

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

type ArtifactFileHandlers struct {
	RootDir         string
	MaxUploadBytes  int64
	TransferTimeout time.Duration
}

type artifactFileResponse struct {
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	Overwritten bool   `json:"overwritten"`
}

func (h *ArtifactFileHandlers) Download(w http.ResponseWriter, r *http.Request) {
	_, fullPath, err := h.resolvePath(r.PathValue("rest"))
	if err != nil {
		WriteValidationProblem(w, r, err.Error(), nil)
		return
	}

	file, err := os.Open(fullPath)
	if errors.Is(err, os.ErrNotExist) {
		WriteProblem(w, r, http.StatusNotFound, "not_found", "File not found", "")
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	if info.IsDir() {
		WriteProblem(w, r, http.StatusNotFound, "not_found", "File not found", "")
		return
	}

	h.extendTransferDeadlines(w)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", path.Base(fullPath)))
	if contentType := mime.TypeByExtension(filepath.Ext(fullPath)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	http.ServeContent(w, r, path.Base(fullPath), info.ModTime(), file)
}

func (h *ArtifactFileHandlers) Upload(w http.ResponseWriter, r *http.Request) {
	relativePath, fullPath, err := h.resolvePath(r.PathValue("rest"))
	if err != nil {
		WriteValidationProblem(w, r, err.Error(), nil)
		return
	}
	if h.MaxUploadBytes <= 0 {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}

	if err := os.MkdirAll(filepath.Dir(fullPath), 0o2775); err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}

	overwritten := false
	if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
		overwritten = true
	}

	h.extendTransferDeadlines(w)
	tmpFile, err := os.CreateTemp(filepath.Dir(fullPath), ".upload-*")
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	tmpPath := tmpFile.Name()
	defer func() {
		tmpFile.Close()
		_ = os.Remove(tmpPath)
	}()

	limitedReader := io.LimitReader(r.Body, h.MaxUploadBytes+1)
	written, err := io.Copy(tmpFile, limitedReader)
	if err != nil {
		WriteProblem(w, r, http.StatusBadRequest, "invalid_body", "Request body could not be read", "")
		return
	}
	if written > h.MaxUploadBytes {
		WriteProblem(w, r, http.StatusRequestEntityTooLarge, "payload_too_large", "File exceeds the configured upload limit", "")
		return
	}
	if err := tmpFile.Sync(); err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	if err := tmpFile.Close(); err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	// CreateTemp uses 0600; widen so host operators can read uploads
	// (dirs under /data/zon/files are 2775 with shared fsGroup 20000).
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	if err := os.Rename(tmpPath, fullPath); err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}

	status := http.StatusCreated
	if overwritten {
		status = http.StatusOK
	}
	writeJSON(w, status, artifactFileResponse{
		Path:        relativePath,
		Size:        written,
		Overwritten: overwritten,
	})
}

func (h *ArtifactFileHandlers) extendTransferDeadlines(w http.ResponseWriter) {
	if h.TransferTimeout <= 0 {
		return
	}
	controller := http.NewResponseController(w)
	deadline := time.Now().Add(h.TransferTimeout)
	_ = controller.SetReadDeadline(deadline)
	_ = controller.SetWriteDeadline(deadline)
}

func (h *ArtifactFileHandlers) resolvePath(raw string) (string, string, error) {
	root := strings.TrimSpace(h.RootDir)
	if root == "" {
		return "", "", fmt.Errorf("filesRootDir is not configured")
	}
	if !strings.HasPrefix(root, "/") {
		return "", "", fmt.Errorf("filesRootDir must be an absolute path")
	}

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", fmt.Errorf("file path is required")
	}
	cleaned := strings.TrimPrefix(path.Clean("/"+trimmed), "/")
	if cleaned == "" || cleaned == "." {
		return "", "", fmt.Errorf("file path is required")
	}

	fullPath := filepath.Join(root, filepath.FromSlash(cleaned))
	relative, err := filepath.Rel(root, fullPath)
	if err != nil {
		return "", "", fmt.Errorf("invalid file path")
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", "", fmt.Errorf("invalid file path")
	}
	return cleaned, fullPath, nil
}

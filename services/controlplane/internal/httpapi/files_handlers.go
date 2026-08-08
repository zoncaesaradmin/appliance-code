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
	"sort"
	"strings"
	"time"

	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/storage"
)

type FileHandlers struct {
	RootDir         string
	MaxUploadBytes  int64
	TransferTimeout time.Duration
	Audit           *audit.Recorder
}

type fileResponse struct {
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	Overwritten bool   `json:"overwritten"`
}

type fileEntry struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Type       string `json:"type"` // "file" or "directory"
	Size       int64  `json:"sizeBytes"`
	ModifiedAt string `json:"modifiedAt"`
}

type fileListResponse struct {
	Path  string      `json:"path"`
	Items []fileEntry `json:"items"`
}

// Get serves a file download, or a directory listing when the path is empty
// or refers to a directory.
func (h *FileHandlers) Get(w http.ResponseWriter, r *http.Request) {
	relativePath, fullPath, err := h.resolvePathAllowRoot(r.PathValue("rest"))
	if err != nil {
		WriteValidationProblem(w, r, err.Error(), nil)
		return
	}

	info, err := os.Stat(fullPath)
	if errors.Is(err, os.ErrNotExist) {
		WriteProblem(w, r, http.StatusNotFound, "not_found", "File not found", "")
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	if info.IsDir() {
		h.writeList(w, r, relativePath, fullPath)
		return
	}

	file, err := os.Open(fullPath)
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	defer file.Close()

	if err := h.extendTransferDeadlines(w); err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", path.Base(fullPath)))
	if contentType := mime.TypeByExtension(filepath.Ext(fullPath)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	http.ServeContent(w, r, path.Base(fullPath), info.ModTime(), file)
}

// Download keeps the previous handler name for older call sites/tests.
func (h *FileHandlers) Download(w http.ResponseWriter, r *http.Request) {
	h.Get(w, r)
}

func (h *FileHandlers) writeList(w http.ResponseWriter, r *http.Request, relativePath, fullPath string) {
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	items := make([]fileEntry, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		name := entry.Name()
		childPath := name
		if relativePath != "" {
			childPath = path.Join(relativePath, name)
		}
		kind := "file"
		size := info.Size()
		if entry.IsDir() {
			kind = "directory"
			size = 0
		}
		items = append(items, fileEntry{
			Name:       name,
			Path:       childPath,
			Type:       kind,
			Size:       size,
			ModifiedAt: info.ModTime().UTC().Format(time.RFC3339),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Type != items[j].Type {
			return items[i].Type == "directory"
		}
		return items[i].Name < items[j].Name
	})
	writeJSON(w, http.StatusOK, fileListResponse{Path: relativePath, Items: items})
}

func (h *FileHandlers) Upload(w http.ResponseWriter, r *http.Request) {
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

	if err := h.extendTransferDeadlines(w); err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
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

	if h.Audit != nil {
		principal, _ := PrincipalFromContext(r.Context())
		if err := h.Audit.Record(r.Context(), principal.Actor(requestIDFromRequest(r), r.RemoteAddr), audit.Event{
			Action: "files.write", TargetType: "file", TargetID: relativePath,
			Outcome: storage.AuditOutcomeSuccess,
			Details: map[string]any{"size": written, "overwritten": overwritten},
		}); err != nil {
			WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
			return
		}
	}

	status := http.StatusCreated
	if overwritten {
		status = http.StatusOK
	}
	writeJSON(w, status, fileResponse{
		Path:        relativePath,
		Size:        written,
		Overwritten: overwritten,
	})
}

func (h *FileHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	relativePath, fullPath, err := h.resolvePath(r.PathValue("rest"))
	if err != nil {
		WriteValidationProblem(w, r, err.Error(), nil)
		return
	}
	info, err := os.Stat(fullPath)
	if errors.Is(err, os.ErrNotExist) {
		WriteProblem(w, r, http.StatusNotFound, "not_found", "File not found", "")
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	if info.IsDir() {
		if err := os.RemoveAll(fullPath); err != nil {
			WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
			return
		}
	} else if err := os.Remove(fullPath); err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	if h.Audit != nil {
		principal, _ := PrincipalFromContext(r.Context())
		if err := h.Audit.Record(r.Context(), principal.Actor(requestIDFromRequest(r), r.RemoteAddr), audit.Event{
			Action: "files.delete", TargetType: "file", TargetID: relativePath,
			Outcome: storage.AuditOutcomeSuccess,
			Details: map[string]any{"directory": info.IsDir()},
		}); err != nil {
			WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *FileHandlers) extendTransferDeadlines(w http.ResponseWriter) error {
	if h.TransferTimeout <= 0 {
		return nil
	}
	controller := http.NewResponseController(w)
	deadline := time.Now().Add(h.TransferTimeout)
	if err := controller.SetReadDeadline(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return fmt.Errorf("set read deadline: %w", err)
	}
	if err := controller.SetWriteDeadline(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return fmt.Errorf("set write deadline: %w", err)
	}
	return nil
}

func (h *FileHandlers) resolvePath(raw string) (string, string, error) {
	relative, full, err := h.resolvePathAllowRoot(raw)
	if err != nil {
		return "", "", err
	}
	if relative == "" {
		return "", "", fmt.Errorf("file path is required")
	}
	return relative, full, nil
}

func (h *FileHandlers) resolvePathAllowRoot(raw string) (string, string, error) {
	root := strings.TrimSpace(h.RootDir)
	if root == "" {
		return "", "", fmt.Errorf("filesRootDir is not configured")
	}
	if !strings.HasPrefix(root, "/") {
		return "", "", fmt.Errorf("filesRootDir must be an absolute path")
	}

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "." || trimmed == "/" {
		return "", root, nil
	}
	cleaned := strings.TrimPrefix(path.Clean("/"+trimmed), "/")
	if cleaned == "" || cleaned == "." {
		return "", root, nil
	}

	fullPath := filepath.Join(root, filepath.FromSlash(cleaned))
	relative, err := filepath.Rel(root, fullPath)
	if err != nil {
		return "", "", fmt.Errorf("invalid file path")
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", "", fmt.Errorf("invalid file path")
	}
	return filepath.ToSlash(relative), fullPath, nil
}

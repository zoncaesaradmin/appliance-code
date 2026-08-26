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
	"appliance-code/services/controlplane/internal/blobstore"
	"appliance-code/services/controlplane/internal/storage"
)

// FileHandlers serves the files capability over a blob-storage object prefix.
// Physical backing paths never become part of the public API.
type FileHandlers struct {
	Store           *blobstore.Client
	ObjectPrefix    string
	MaxUploadBytes  int64
	TransferTimeout time.Duration
	Audit           *audit.Recorder
}

// Get serves a file download, or a directory listing when the path is empty
// or refers to a virtual directory prefix.
func (h *FileHandlers) Get(w http.ResponseWriter, r *http.Request) {
	relativePath, err := blobRelativePath(r.PathValue("rest"), false)
	if err != nil {
		WriteValidationProblem(w, r, err.Error(), nil)
		return
	}
	if err := h.Store.EnsureBucket(r.Context()); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	if relativePath == "" {
		h.writeList(w, r, "")
		return
	}
	object, err := h.Store.Stat(r.Context(), h.objectKey(relativePath))
	if errors.Is(err, blobstore.ErrNotFound) {
		list, listErr := h.Store.List(r.Context(), h.directoryPrefix(relativePath))
		if listErr != nil {
			h.writeStoreError(w, r, listErr)
			return
		}
		if len(list.Objects) == 0 && len(list.CommonPrefixes) == 0 {
			WriteProblem(w, r, http.StatusNotFound, "not_found", "File not found", "")
			return
		}
		h.writeListResult(w, relativePath, list)
		return
	}
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	h.stream(w, r, relativePath, object)
}

// Download keeps the previous handler name for older call sites/tests.
func (h *FileHandlers) Download(w http.ResponseWriter, r *http.Request) {
	h.Get(w, r)
}

func (h *FileHandlers) stream(w http.ResponseWriter, r *http.Request, relativePath string, object blobstore.Object) {
	if err := h.extendTransferDeadlines(w); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	response, got, err := h.Store.Get(r.Context(), h.objectKey(relativePath), r.Header.Get("Range"))
	if errors.Is(err, blobstore.ErrNotFound) {
		WriteProblem(w, r, http.StatusNotFound, "not_found", "File not found", "")
		return
	}
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	defer response.Body.Close()
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", path.Base(relativePath)))
	contentType := strings.TrimSpace(object.ContentType)
	if contentType == "" {
		contentType = strings.TrimSpace(got.ContentType)
	}
	if contentType == "" {
		contentType = mime.TypeByExtension(filepath.Ext(relativePath))
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	for _, header := range []string{"Content-Length", "Content-Range", "Last-Modified", "ETag"} {
		if value := response.Header.Get(header); value != "" {
			w.Header().Set(header, value)
		}
	}
	if got.Size >= 0 && w.Header().Get("Content-Length") == "" {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", got.Size))
	}
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
}

func (h *FileHandlers) Upload(w http.ResponseWriter, r *http.Request) {
	relativePath, err := blobRelativePath(r.PathValue("rest"), true)
	if err != nil {
		WriteValidationProblem(w, r, err.Error(), nil)
		return
	}
	if h.MaxUploadBytes <= 0 {
		h.writeStoreError(w, r, fmt.Errorf("files upload limit is not configured"))
		return
	}
	if err := h.extendTransferDeadlines(w); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	temporary, err := os.CreateTemp("", "files-upload-*")
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	temporaryPath := temporary.Name()
	defer func() { _ = temporary.Close(); _ = os.Remove(temporaryPath) }()
	written, err := io.Copy(temporary, io.LimitReader(r.Body, h.MaxUploadBytes+1))
	if err != nil {
		WriteProblem(w, r, http.StatusBadRequest, "invalid_body", "Request body could not be read", "")
		return
	}
	if written > h.MaxUploadBytes {
		WriteProblem(w, r, http.StatusRequestEntityTooLarge, "payload_too_large", "File exceeds the configured upload limit", "")
		return
	}
	if err := temporary.Sync(); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	if err := temporary.Close(); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	if err := h.Store.EnsureBucket(r.Context()); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	_, statErr := h.Store.Stat(r.Context(), h.objectKey(relativePath))
	overwritten := statErr == nil
	if statErr != nil && !errors.Is(statErr, blobstore.ErrNotFound) {
		h.writeStoreError(w, r, statErr)
		return
	}
	input, err := os.Open(temporaryPath)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	defer input.Close()
	contentType := mime.TypeByExtension(filepath.Ext(relativePath))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if _, err := h.Store.Put(r.Context(), h.objectKey(relativePath), input, written, contentType); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	h.record(r, "files.write", relativePath, map[string]any{"size": written, "overwritten": overwritten})
	status := http.StatusCreated
	if overwritten {
		status = http.StatusOK
	}
	writeJSON(w, status, fileResponse{Path: relativePath, Size: written, Overwritten: overwritten})
}

func (h *FileHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	relativePath, err := blobRelativePath(r.PathValue("rest"), true)
	if err != nil {
		WriteValidationProblem(w, r, err.Error(), nil)
		return
	}
	key := h.objectKey(relativePath)
	_, statErr := h.Store.Stat(r.Context(), key)
	directory := false
	if errors.Is(statErr, blobstore.ErrNotFound) {
		directory = true
		items, err := h.Store.ListAll(r.Context(), h.directoryPrefix(relativePath))
		if err != nil {
			h.writeStoreError(w, r, err)
			return
		}
		if len(items.Objects) == 0 {
			WriteProblem(w, r, http.StatusNotFound, "not_found", "File not found", "")
			return
		}
		for _, item := range items.Objects {
			if err := h.Store.Delete(r.Context(), item.Key); err != nil {
				h.writeStoreError(w, r, err)
				return
			}
		}
	} else if statErr != nil {
		h.writeStoreError(w, r, statErr)
		return
	} else if err := h.Store.Delete(r.Context(), key); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	h.record(r, "files.delete", relativePath, map[string]any{"directory": directory})
	w.WriteHeader(http.StatusNoContent)
}

func (h *FileHandlers) writeList(w http.ResponseWriter, r *http.Request, relativePath string) {
	result, err := h.Store.List(r.Context(), h.directoryPrefix(relativePath))
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	h.writeListResult(w, relativePath, result)
}

func (h *FileHandlers) writeListResult(w http.ResponseWriter, relativePath string, result blobstore.ListResult) {
	items := make([]fileEntry, 0, len(result.Objects)+len(result.CommonPrefixes))
	prefix := h.directoryPrefix(relativePath)
	for _, commonPrefix := range result.CommonPrefixes {
		name := strings.TrimSuffix(strings.TrimPrefix(commonPrefix, prefix), "/")
		if name == "" {
			continue
		}
		itemPath := name
		if relativePath != "" {
			itemPath = path.Join(relativePath, name)
		}
		items = append(items, fileEntry{Name: name, Path: itemPath, Type: "directory", ModifiedAt: ""})
	}
	for _, object := range result.Objects {
		name := strings.TrimPrefix(object.Key, prefix)
		if name == "" || strings.Contains(name, "/") {
			continue
		}
		itemPath := name
		if relativePath != "" {
			itemPath = path.Join(relativePath, name)
		}
		items = append(items, fileEntry{
			Name:       name,
			Path:       itemPath,
			Type:       "file",
			Size:       object.Size,
			ModifiedAt: object.ModifiedAt.UTC().Format(time.RFC3339),
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

func (h *FileHandlers) objectKey(relativePath string) string {
	return strings.Trim(h.ObjectPrefix, "/") + "/" + relativePath
}

func (h *FileHandlers) directoryPrefix(relativePath string) string {
	prefix := strings.Trim(h.ObjectPrefix, "/") + "/"
	if relativePath != "" {
		prefix += strings.Trim(relativePath, "/") + "/"
	}
	return prefix
}

func (h *FileHandlers) record(r *http.Request, action, target string, details map[string]any) {
	if h.Audit == nil {
		return
	}
	principal, _ := PrincipalFromContext(r.Context())
	_ = h.Audit.Record(r.Context(), principal.Actor(requestIDFromRequest(r), r.RemoteAddr), audit.Event{
		Action: action, TargetType: "file", TargetID: target, Outcome: storage.AuditOutcomeSuccess, Details: details,
	})
}

func (h *FileHandlers) extendTransferDeadlines(w http.ResponseWriter) error {
	if h.TransferTimeout <= 0 {
		return nil
	}
	controller := http.NewResponseController(w)
	deadline := time.Now().Add(h.TransferTimeout)
	if err := controller.SetReadDeadline(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	if err := controller.SetWriteDeadline(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	return nil
}

func (h *FileHandlers) writeStoreError(w http.ResponseWriter, r *http.Request, err error) {
	WriteProblem(w, r, http.StatusInternalServerError, "blob_storage_unavailable", "Blob storage is unavailable", "")
}

// Shared response types used by files and video library handlers.
type fileResponse struct {
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	Overwritten bool   `json:"overwritten"`
	Status      string `json:"status,omitempty"`
}

type fileEntry struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Type       string `json:"type"` // "file" or "directory"
	Size       int64  `json:"sizeBytes"`
	ModifiedAt string `json:"modifiedAt"`
	Status     string `json:"status,omitempty"`
}

type fileListResponse struct {
	Path  string      `json:"path"`
	Items []fileEntry `json:"items"`
}

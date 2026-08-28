package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	"appliance-code/services/controlplane/internal/videomedia"
)

// VideoLibraryHandlers maps the public video-library hierarchy onto a generic
// blob prefix. Physical backing paths never become part of the video API.
type VideoLibraryHandlers struct {
	Store           *blobstore.Client
	ObjectPrefix    string
	ProjectionDir   string
	MaxUploadBytes  int64
	TransferTimeout time.Duration
	Audit           *audit.Recorder
}

func (h *VideoLibraryHandlers) Get(w http.ResponseWriter, r *http.Request) {
	relativePath, err := blobRelativePath(r.PathValue("rest"), false)
	if err != nil {
		WriteValidationProblem(w, r, err.Error(), nil)
		return
	}
	// A new local S3 service has no buckets until its first successful write.
	// Browsing an empty video library must create the configured bucket instead
	// of surfacing MinIO's NoSuchBucket response as a storage outage.
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

func (h *VideoLibraryHandlers) stream(w http.ResponseWriter, r *http.Request, relativePath string, _ blobstore.Object) {
	if err := h.extendTransferDeadlines(w); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	response, object, err := h.Store.Get(r.Context(), h.objectKey(relativePath), r.Header.Get("Range"))
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
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", path.Base(relativePath)))
	w.Header().Set("Content-Type", "video/mp4")
	for _, header := range []string{"Content-Length", "Content-Range", "Last-Modified", "ETag"} {
		if value := response.Header.Get(header); value != "" {
			w.Header().Set(header, value)
		}
	}
	if object.Size >= 0 && w.Header().Get("Content-Length") == "" {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", object.Size))
	}
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
}

func (h *VideoLibraryHandlers) Upload(w http.ResponseWriter, r *http.Request) {
	relativePath, err := blobRelativePath(r.PathValue("rest"), true)
	if err != nil {
		WriteValidationProblem(w, r, err.Error(), nil)
		return
	}
	if !strings.EqualFold(filepath.Ext(relativePath), ".mp4") {
		WriteValidationProblem(w, r, "video library uploads must use a .mp4 destination path", nil)
		return
	}
	if h.MaxUploadBytes <= 0 {
		h.writeStoreError(w, r, fmt.Errorf("video upload limit is not configured"))
		return
	}
	if err := h.extendTransferDeadlines(w); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	temporary, err := os.CreateTemp("", "video-upload-*.mp4")
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
	if err := videomedia.ValidateBrowserMP4(temporaryPath); err != nil {
		WriteValidationProblem(w, r, err.Error(), nil)
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
	if _, err := h.Store.Put(r.Context(), h.objectKey(relativePath), input, written, "video/mp4"); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	if err := h.materialize(temporaryPath, relativePath); err != nil {
		// The blob store is authoritative. Roll back this just-written object so
		// a success response never leaves Jellyfin with a missing media entry.
		_ = h.Store.Delete(r.Context(), h.objectKey(relativePath))
		h.writeStoreError(w, r, fmt.Errorf("materialize video library projection: %w", err))
		return
	}
	h.record(r, "video.library.write", relativePath, map[string]any{"size": written, "overwritten": overwritten})
	status := http.StatusCreated
	if overwritten {
		status = http.StatusOK
	}
	writeJSON(w, status, fileResponse{Path: relativePath, Size: written, Overwritten: overwritten, Status: "ready"})
}

func (h *VideoLibraryHandlers) Delete(w http.ResponseWriter, r *http.Request) {
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
	if err := h.removeMaterialized(relativePath, directory); err != nil {
		h.writeStoreError(w, r, fmt.Errorf("remove video library projection: %w", err))
		return
	}
	h.record(r, "video.library.delete", relativePath, map[string]any{"directory": directory})
	w.WriteHeader(http.StatusNoContent)
}

// SyncProjection materializes objects that predate the local media view. It
// runs only inside the control plane; application workloads never receive S3
// credentials or write access to this directory.
func (h *VideoLibraryHandlers) SyncProjection(ctx context.Context) error {
	if strings.TrimSpace(h.ProjectionDir) == "" {
		return fmt.Errorf("video media projection directory is not configured")
	}
	if err := h.Store.EnsureBucket(ctx); err != nil {
		return err
	}
	items, err := h.Store.ListAll(ctx, h.directoryPrefix(""))
	if err != nil {
		return err
	}
	for _, item := range items.Objects {
		relativePath := strings.TrimPrefix(item.Key, h.directoryPrefix(""))
		if relativePath == "" || strings.Contains(relativePath, "..") {
			continue
		}
		response, _, err := h.Store.Get(ctx, item.Key, "")
		if err != nil {
			return err
		}
		if err := h.materializeReader(response.Body, relativePath); err != nil {
			_ = response.Body.Close()
			return err
		}
		if err := response.Body.Close(); err != nil {
			return err
		}
	}
	return nil
}

func (h *VideoLibraryHandlers) materialize(sourcePath, relativePath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	return h.materializeReader(source, relativePath)
}

func (h *VideoLibraryHandlers) materializeReader(source io.Reader, relativePath string) error {
	target, err := h.projectionPath(relativePath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o2770); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".video-media-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := io.Copy(temporary, source); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Chmod(0o640); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, target)
}

func (h *VideoLibraryHandlers) removeMaterialized(relativePath string, directory bool) error {
	target, err := h.projectionPath(relativePath)
	if err != nil {
		return err
	}
	if directory {
		return os.RemoveAll(target)
	}
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (h *VideoLibraryHandlers) projectionPath(relativePath string) (string, error) {
	root := filepath.Clean(strings.TrimSpace(h.ProjectionDir))
	if !filepath.IsAbs(root) || root == "/" {
		return "", fmt.Errorf("invalid video media projection directory")
	}
	clean := filepath.Clean(filepath.FromSlash(relativePath))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid video projection path")
	}
	return filepath.Join(root, clean), nil
}

func (h *VideoLibraryHandlers) writeList(w http.ResponseWriter, r *http.Request, relativePath string) {
	result, err := h.Store.List(r.Context(), h.directoryPrefix(relativePath))
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	h.writeListResult(w, relativePath, result)
}

func (h *VideoLibraryHandlers) writeListResult(w http.ResponseWriter, relativePath string, result blobstore.ListResult) {
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
		items = append(items, fileEntry{Name: name, Path: itemPath, Type: "file", Size: object.Size, ModifiedAt: object.ModifiedAt.UTC().Format(time.RFC3339), Status: "ready"})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Type != items[j].Type {
			return items[i].Type == "directory"
		}
		return items[i].Name < items[j].Name
	})
	writeJSON(w, http.StatusOK, fileListResponse{Path: relativePath, Items: items})
}

func (h *VideoLibraryHandlers) objectKey(relativePath string) string {
	return strings.Trim(h.ObjectPrefix, "/") + "/" + relativePath
}
func (h *VideoLibraryHandlers) directoryPrefix(relativePath string) string {
	prefix := strings.Trim(h.ObjectPrefix, "/") + "/"
	if relativePath != "" {
		prefix += strings.Trim(relativePath, "/") + "/"
	}
	return prefix
}
func (h *VideoLibraryHandlers) record(r *http.Request, action, target string, details map[string]any) {
	if h.Audit == nil {
		return
	}
	principal, _ := PrincipalFromContext(r.Context())
	_ = h.Audit.Record(r.Context(), principal.Actor(requestIDFromRequest(r), r.RemoteAddr), audit.Event{Action: action, TargetType: "file", TargetID: target, Outcome: storage.AuditOutcomeSuccess, Details: details})
}
func (h *VideoLibraryHandlers) extendTransferDeadlines(w http.ResponseWriter) error {
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
func (h *VideoLibraryHandlers) writeStoreError(w http.ResponseWriter, r *http.Request, err error) {
	WriteProblem(w, r, http.StatusInternalServerError, "blob_storage_unavailable", "Blob storage is unavailable", "")
}

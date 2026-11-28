// Package geodata exposes the Geo editor's narrowly scoped persistence API.
// The caller must authenticate with a target-generated internal token; browser
// authentication and role checks remain the control plane's responsibility.
package geodata

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"compress/gzip"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const InternalAuthHeader = "X-Appliance-Internal-Auth"

type SaveRequest struct {
	Name        string          `json:"p_name"`
	Description string          `json:"p_description"`
	Geometry    json.RawMessage `json:"p_geometry"`
	ID          *string         `json:"p_id"`
	Revision    *int            `json:"p_revision"`
}

type Store interface {
	List(context.Context) (json.RawMessage, error)
	Export(context.Context) (json.RawMessage, error)
	Save(context.Context, SaveRequest) (string, error)
	Ready(context.Context) error
}

// SnapshotObjectStore is the small object-storage contract needed for Geo
// snapshots. The concrete implementation is the appliance's S3-compatible
// blob client; keeping this interface here makes the export atomic and easy to
// test without coupling the Geo API to a storage implementation.
type SnapshotObjectStore interface {
	Put(context.Context, string, io.Reader, int64, string) (SnapshotObject, error)
}

type SnapshotObject struct {
	Key  string
	Size int64
}

type SnapshotExport struct {
	SchemaVersion    int    `json:"schemaVersion"`
	SnapshotID       string `json:"snapshotId"`
	ExportedAt       string `json:"exportedAt"`
	ZoneCount        int    `json:"zoneCount"`
	ContentKey       string `json:"contentKey"`
	ManifestKey      string `json:"manifestKey"`
	CurrentKey       string `json:"currentKey"`
	ContentSHA256    string `json:"contentSha256"`
	ContentBytes     int64  `json:"contentBytes"`
	CompressedBytes  int64  `json:"compressedBytes"`
}

func NewHandler(store Store, internalToken string) (http.Handler, error) {
	return NewHandlerWithSnapshotStore(store, internalToken, nil, "")
}

func NewHandlerWithSnapshotStore(store Store, internalToken string, snapshots SnapshotObjectStore, snapshotPrefix string) (http.Handler, error) {
	if store == nil || strings.TrimSpace(internalToken) == "" {
		return nil, errors.New("geo data store and internal token are required")
	}
	if snapshots != nil && strings.Trim(strings.TrimSpace(snapshotPrefix), "/") == "" {
		snapshotPrefix = "geo/zones"
	}
	expected := sha256.Sum256([]byte(internalToken))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		if err := store.Ready(r.Context()); err != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	protect := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			actual := sha256.Sum256([]byte(r.Header.Get(InternalAuthHeader)))
			if subtle.ConstantTimeCompare(actual[:], expected[:]) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next(w, r)
		}
	}
	mux.HandleFunc("GET /api/v1/geo/zones", protect(func(w http.ResponseWriter, r *http.Request) {
		zones, err := store.List(r.Context())
		if err != nil {
			http.Error(w, "zone data unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(zones)
	}))
	mux.HandleFunc("POST /api/v1/geo/snapshots/export", protect(func(w http.ResponseWriter, r *http.Request) {
		if snapshots == nil {
			http.Error(w, "snapshot export is not configured", http.StatusServiceUnavailable)
			return
		}
		export, err := exportSnapshot(r.Context(), store, snapshots, snapshotPrefix, time.Now)
		if err != nil {
			http.Error(w, "could not export zone snapshot", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(export)
	}))
	mux.HandleFunc("POST /api/v1/geo/editor/rpc/save_zone", protect(func(w http.ResponseWriter, r *http.Request) {
		var request SaveRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			http.Error(w, "invalid zone request", http.StatusBadRequest)
			return
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			http.Error(w, "invalid zone request", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(request.Name) == "" || len(request.Name) > 120 || len(request.Geometry) == 0 || (request.ID == nil) != (request.Revision == nil) {
			http.Error(w, "invalid zone request", http.StatusBadRequest)
			return
		}
		id, err := store.Save(r.Context(), request)
		if err != nil {
			http.Error(w, "could not save zone", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(id)
	}))
	return mux, nil
}

func exportSnapshot(ctx context.Context, store Store, snapshots SnapshotObjectStore, prefix string, now func() time.Time) (SnapshotExport, error) {
	content, err := store.Export(ctx)
	if err != nil {
		return SnapshotExport{}, err
	}
	var collection struct {
		Type     string            `json:"type"`
		Features []json.RawMessage `json:"features"`
	}
	if err := json.Unmarshal(content, &collection); err != nil || collection.Type != "FeatureCollection" || collection.Features == nil {
		return SnapshotExport{}, fmt.Errorf("invalid zone export")
	}
	content = bytes.TrimSpace(content)
	digest := sha256.Sum256(content)
	digestHex := hex.EncodeToString(digest[:])
	exportedAt := now().UTC()
	snapshotID := exportedAt.Format("20060102T150405Z") + "-" + digestHex[:16]
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	base := prefix + "/" + snapshotID
	contentKey := base + ".geojson.gz"
	manifestKey := base + ".manifest.json"
	currentKey := prefix + "/current.json"

	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(content); err != nil {
		return SnapshotExport{}, fmt.Errorf("compress zone export: %w", err)
	}
	if err := writer.Close(); err != nil {
		return SnapshotExport{}, fmt.Errorf("finish zone export compression: %w", err)
	}
	if _, err := snapshots.Put(ctx, contentKey, bytes.NewReader(compressed.Bytes()), int64(compressed.Len()), "application/geo+json"); err != nil {
		return SnapshotExport{}, fmt.Errorf("upload zone snapshot: %w", err)
	}
	result := SnapshotExport{
		SchemaVersion:   1,
		SnapshotID:      snapshotID,
		ExportedAt:      exportedAt.Format(time.RFC3339Nano),
		ZoneCount:       len(collection.Features),
		ContentKey:      contentKey,
		ManifestKey:     manifestKey,
		CurrentKey:      currentKey,
		ContentSHA256:   digestHex,
		ContentBytes:    int64(len(content)),
		CompressedBytes: int64(compressed.Len()),
	}
	manifest, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return SnapshotExport{}, fmt.Errorf("encode zone snapshot manifest: %w", err)
	}
	if _, err := snapshots.Put(ctx, manifestKey, bytes.NewReader(manifest), int64(len(manifest)), "application/json"); err != nil {
		return SnapshotExport{}, fmt.Errorf("upload zone snapshot manifest: %w", err)
	}
	current, err := json.Marshal(result)
	if err != nil {
		return SnapshotExport{}, fmt.Errorf("encode current zone snapshot pointer: %w", err)
	}
	if _, err := snapshots.Put(ctx, currentKey, bytes.NewReader(current), int64(len(current)), "application/json"); err != nil {
		return SnapshotExport{}, fmt.Errorf("publish current zone snapshot: %w", err)
	}
	return result, nil
}

// Package geodata exposes the Geo editor's narrowly scoped persistence API.
// The caller must authenticate with a target-generated internal token; browser
// authentication and role checks remain the control plane's responsibility.
package geodata

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"appliance-code/sdk/golang/geosnapshot"
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
type SnapshotObjectStore = geosnapshot.Store
type SnapshotObject = geosnapshot.SnapshotObject
type SnapshotExport = geosnapshot.Manifest

type SnapshotList struct {
	Prefix    string           `json:"prefix"`
	Snapshots []SnapshotExport `json:"snapshots"`
}

func NewHandler(store Store, internalToken string) (http.Handler, error) {
	return NewHandlerWithSnapshotStore(store, internalToken, nil, "")
}

func NewHandlerWithSnapshotStore(store Store, internalToken string, snapshots SnapshotObjectStore, snapshotPrefix string) (http.Handler, error) {
	if store == nil || strings.TrimSpace(internalToken) == "" {
		return nil, errors.New("geo data store and internal token are required")
	}
	if snapshots != nil {
		snapshotPrefix = strings.Trim(strings.TrimSpace(snapshotPrefix), "/")
		if snapshotPrefix == "" {
			snapshotPrefix = "geo/zones"
		}
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
	mux.HandleFunc("GET /api/v1/geo/snapshots", protect(func(w http.ResponseWriter, r *http.Request) {
		if snapshots == nil {
			http.Error(w, "snapshot export is not configured", http.StatusServiceUnavailable)
			return
		}
		list, err := listSnapshots(r.Context(), snapshots, snapshotPrefix)
		if err != nil {
			http.Error(w, "could not list zone snapshots", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
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
	return geosnapshot.Write(ctx, snapshots, prefix, content, now())
}

func listSnapshots(ctx context.Context, snapshots SnapshotObjectStore, prefix string) (SnapshotList, error) {
	keys, err := snapshots.List(ctx, prefix)
	if err != nil {
		return SnapshotList{}, fmt.Errorf("list snapshot objects: %w", err)
	}
	result := SnapshotList{Prefix: prefix, Snapshots: make([]SnapshotExport, 0)}
	for _, key := range keys {
		if !strings.HasSuffix(key, ".manifest.json") {
			continue
		}
		body, err := snapshots.Get(ctx, key)
		if err != nil {
			return SnapshotList{}, fmt.Errorf("read snapshot manifest: %w", err)
		}
		payload, readErr := io.ReadAll(io.LimitReader(body, 1<<20+1))
		closeErr := body.Close()
		if readErr != nil {
			return SnapshotList{}, fmt.Errorf("read snapshot manifest: %w", readErr)
		}
		if closeErr != nil {
			return SnapshotList{}, fmt.Errorf("close snapshot manifest: %w", closeErr)
		}
		if len(payload) > 1<<20 {
			return SnapshotList{}, errors.New("snapshot manifest is too large")
		}
		var snapshot SnapshotExport
		if err := json.Unmarshal(payload, &snapshot); err != nil {
			return SnapshotList{}, fmt.Errorf("decode snapshot manifest: %w", err)
		}
		if snapshot.SchemaVersion != 1 || snapshot.SnapshotID == "" || snapshot.ContentKey == "" || snapshot.ManifestKey != key {
			return SnapshotList{}, fmt.Errorf("invalid snapshot manifest %q", key)
		}
		result.Snapshots = append(result.Snapshots, snapshot)
	}
	sort.Slice(result.Snapshots, func(i, j int) bool {
		if result.Snapshots[i].ExportedAt == result.Snapshots[j].ExportedAt {
			return result.Snapshots[i].SnapshotID > result.Snapshots[j].SnapshotID
		}
		return result.Snapshots[i].ExportedAt > result.Snapshots[j].ExportedAt
	})
	return result, nil
}

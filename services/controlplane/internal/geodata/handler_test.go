package geodata

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeStore struct{ saved SaveRequest }

func (s *fakeStore) List(context.Context) (json.RawMessage, error) {
	return json.RawMessage(`[{"name":"test"}]`), nil
}
func (s *fakeStore) Export(context.Context) (json.RawMessage, error) {
	return json.RawMessage(`{"type":"FeatureCollection","features":[{"type":"Feature","id":"zone-id","properties":{"name":"test"},"geometry":{"type":"Polygon","coordinates":[]}}]}`), nil
}
func (s *fakeStore) Save(_ context.Context, r SaveRequest) (string, error) {
	s.saved = r
	return "zone-id", nil
}
func (s *fakeStore) Ready(context.Context) error { return nil }

type fakeSnapshotStore struct {
	objects     map[string][]byte
	contentType map[string]string
}

func (s *fakeSnapshotStore) Put(_ context.Context, key string, body io.Reader, size int64, contentType string) (SnapshotObject, error) {
	if s.objects == nil {
		s.objects = make(map[string][]byte)
	}
	if s.contentType == nil {
		s.contentType = make(map[string]string)
	}
	content, err := io.ReadAll(body)
	if err != nil {
		return SnapshotObject{}, err
	}
	if int64(len(content)) != size {
		return SnapshotObject{}, io.ErrShortBuffer
	}
	s.objects[key] = content
	s.contentType[key] = contentType
	return SnapshotObject{Key: key, Size: size}, nil
}

func TestEditorEndpointsRequireInternalAuthentication(t *testing.T) {
	store := &fakeStore{}
	h, err := NewHandler(store, "test-internal-token")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, token string
		want        int
	}{
		{"missing", "", http.StatusUnauthorized},
		{"wrong", "wrong", http.StatusUnauthorized},
		{"valid", "test-internal-token", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/geo/zones?order=name.asc", nil)
			req.Header.Set(InternalAuthHeader, tc.token)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status %d, want %d", rec.Code, tc.want)
			}
		})
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/geo/editor/rpc/save_zone", strings.NewReader(`{"p_name":"zone","p_description":"","p_geometry":{"type":"Polygon","coordinates":[]},"p_id":null,"p_revision":null}`))
	req.Header.Set(InternalAuthHeader, "test-internal-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `"zone-id"` || store.saved.Name != "zone" {
		t.Fatalf("status %d body %q request %+v", rec.Code, rec.Body.String(), store.saved)
	}
}

func TestSnapshotExportPublishesVerifiableCurrentSnapshot(t *testing.T) {
	store := &fakeStore{}
	snapshots := &fakeSnapshotStore{}
	h, err := NewHandlerWithSnapshotStore(store, "test-internal-token", snapshots, "geo/zones")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/geo/snapshots/export", nil)
	req.Header.Set(InternalAuthHeader, "test-internal-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d, want %d", rec.Code, http.StatusCreated)
	}
	var result SnapshotExport
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != 1 || result.ZoneCount != 1 || result.ContentBytes == 0 || result.CompressedBytes == 0 {
		t.Fatalf("unexpected export result: %+v", result)
	}
	content, ok := snapshots.objects[result.ContentKey]
	if !ok {
		t.Fatalf("snapshot content %q was not uploaded", result.ContentKey)
	}
	if snapshots.contentType[result.ContentKey] != "application/gzip" {
		t.Fatalf("content type %q, want application/gzip", snapshots.contentType[result.ContentKey])
	}
	reader, err := gzip.NewReader(bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(plain)
	if got := hex.EncodeToString(digest[:]); got != result.ContentSHA256 {
		t.Fatalf("content digest %q, want %q", result.ContentSHA256, got)
	}
	var collection struct {
		Type     string            `json:"type"`
		Features []json.RawMessage `json:"features"`
	}
	if err := json.Unmarshal(plain, &collection); err != nil {
		t.Fatal(err)
	}
	if collection.Type != "FeatureCollection" || len(collection.Features) != 1 {
		t.Fatalf("unexpected snapshot content: %s", plain)
	}
	for _, key := range []string{result.ManifestKey, result.CurrentKey} {
		if _, ok := snapshots.objects[key]; !ok {
			t.Fatalf("snapshot pointer %q was not uploaded", key)
		}
	}
	var current SnapshotExport
	if err := json.Unmarshal(snapshots.objects[result.CurrentKey], &current); err != nil {
		t.Fatal(err)
	}
	if current != result {
		t.Fatalf("current pointer %+v does not match response %+v", current, result)
	}
}

func TestSnapshotExportRequiresStorageConfiguration(t *testing.T) {
	h, err := NewHandler(&fakeStore{}, "test-internal-token")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/geo/snapshots/export", nil)
	req.Header.Set(InternalAuthHeader, "test-internal-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

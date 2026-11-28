package geosnapshot

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

type memoryStore struct{ objects map[string][]byte }

func (s *memoryStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	content, ok := s.objects[key]
	if !ok {
		return nil, errors.New("object not found")
	}
	return io.NopCloser(bytes.NewReader(content)), nil
}

func (s *memoryStore) List(_ context.Context, prefix string) ([]string, error) {
	keys := make([]string, 0, len(s.objects))
	for key := range s.objects {
		if prefix == "" || strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func (s *memoryStore) Put(_ context.Context, key string, body io.Reader, size int64, _ string) (SnapshotObject, error) {
	content, err := io.ReadAll(body)
	if err != nil {
		return SnapshotObject{}, err
	}
	if int64(len(content)) != size {
		return SnapshotObject{}, errors.New("incorrect object size")
	}
	if s.objects == nil {
		s.objects = map[string][]byte{}
	}
	s.objects[key] = content
	return SnapshotObject{Key: key, Size: size}, nil
}

type fakeLoader struct {
	zones []Zone
}

func (l *fakeLoader) ReplaceZones(_ context.Context, zones []Zone) (int, error) {
	l.zones = append([]Zone(nil), zones...)
	return len(zones), nil
}

func TestReadAndLoadVerifiesSnapshotContract(t *testing.T) {
	content := []byte(`{"type":"FeatureCollection","features":[{"type":"Feature","id":"zone-1","geometry":{"type":"Polygon","coordinates":[[[77,12],[78,12],[78,13],[77,12]]]},"properties":{"name":"One","description":"first","revision":3}}]}`)
	digest := sha256.Sum256(content)
	manifest := Manifest{
		SchemaVersion:   SchemaVersion,
		SnapshotID:      "20261128T120000Z-deadbeefdeadbeef",
		ExportedAt:      "2026-11-28T12:00:00Z",
		ZoneCount:       1,
		ContentKey:      "geo/zones/20261128T120000Z-deadbeefdeadbeef.geojson.gz",
		ManifestKey:     "geo/zones/20261128T120000Z-deadbeefdeadbeef.manifest.json",
		CurrentKey:      "geo/zones/current.json",
		ContentSHA256:   hex.EncodeToString(digest[:]),
		ContentBytes:    int64(len(content)),
		CompressedBytes: 0,
	}
	compressed := new(bytes.Buffer)
	writer := gzip.NewWriter(compressed)
	if _, err := writer.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	manifest.CompressedBytes = int64(compressed.Len())
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryStore{objects: map[string][]byte{
		manifest.CurrentKey:  manifestBytes,
		manifest.ManifestKey: manifestBytes,
		manifest.ContentKey:  compressed.Bytes(),
	}}
	snapshot, err := Read(context.Background(), store, "geo/zones")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Manifest != manifest || len(snapshot.Zones) != 1 || snapshot.Zones[0].ID != "zone-1" {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	loader := &fakeLoader{}
	result, err := Load(context.Background(), loader, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if result.SnapshotID != manifest.SnapshotID || result.ZoneCount != 1 || len(loader.zones) != 1 {
		t.Fatalf("unexpected load result: %+v zones=%+v", result, loader.zones)
	}
}

func TestWriteAndReadRoundTrip(t *testing.T) {
	content := []byte(`{"type":"FeatureCollection","features":[]}`)
	store := &memoryStore{objects: map[string][]byte{}}
	manifest, err := Write(context.Background(), store, "geo/zones", content, time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := Read(context.Background(), store, "geo/zones")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Manifest != manifest || len(snapshot.Zones) != 0 {
		t.Fatalf("round trip mismatch: manifest=%+v zones=%+v", snapshot.Manifest, snapshot.Zones)
	}
}

func TestReadRejectsDigestMismatch(t *testing.T) {
	content := []byte(`{"type":"FeatureCollection","features":[]}`)
	manifest := Manifest{SchemaVersion: SchemaVersion, SnapshotID: "snapshot", ZoneCount: 0, ContentKey: "geo/zones/snapshot.geojson.gz", ManifestKey: "geo/zones/snapshot.manifest.json", CurrentKey: "geo/zones/current.json", ContentSHA256: "0000000000000000000000000000000000000000000000000000000000000000", ContentBytes: int64(len(content))}
	compressed := new(bytes.Buffer)
	writer := gzip.NewWriter(compressed)
	if _, err := writer.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryStore{objects: map[string][]byte{manifest.CurrentKey: manifestBytes, manifest.ManifestKey: manifestBytes, manifest.ContentKey: compressed.Bytes()}}
	if _, err := Read(context.Background(), store, "geo/zones"); err == nil {
		t.Fatal("Read accepted content with a mismatched digest")
	}
}

func TestPostgresLoaderRequiresDatabase(t *testing.T) {
	if _, err := (PostgresLoader{}).ReplaceZones(context.Background(), nil); err == nil {
		t.Fatal("PostgresLoader accepted a nil database")
	}
}

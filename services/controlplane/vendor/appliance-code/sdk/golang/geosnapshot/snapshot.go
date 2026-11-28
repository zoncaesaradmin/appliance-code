// Package geosnapshot defines the shared Geo-Zone snapshot contract.
//
// A producer writes one gzip-compressed GeoJSON FeatureCollection, a JSON
// manifest, and a current.json pointer. Consumers use Read to fetch and
// verify the complete snapshot before passing it to Load or another ZoneLoader.
package geosnapshot

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	SchemaVersion       = 1
	DefaultPrefix       = "geo/zones"
	MaxManifestBytes    = 1 << 20
	MaxContentBytes     = 256 << 20
	MaxZones            = 10000
	DefaultZoneRevision = 1
)

// Manifest is the metadata written beside each immutable snapshot payload.
// ContentSHA256 is the digest of the uncompressed GeoJSON bytes.
type Manifest struct {
	SchemaVersion   int    `json:"schemaVersion"`
	SnapshotID      string `json:"snapshotId"`
	ExportedAt      string `json:"exportedAt"`
	ZoneCount       int    `json:"zoneCount"`
	ContentKey      string `json:"contentKey"`
	ManifestKey     string `json:"manifestKey"`
	CurrentKey      string `json:"currentKey"`
	ContentSHA256   string `json:"contentSha256"`
	ContentBytes    int64  `json:"contentBytes"`
	CompressedBytes int64  `json:"compressedBytes"`
}

// Zone is the canonical snapshot representation consumed by Postgres loaders.
// Geometry is a GeoJSON Polygon or MultiPolygon object with SRID 4326.
type Zone struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Revision    int             `json:"revision"`
	Geometry    json.RawMessage `json:"geometry"`
}

type Snapshot struct {
	Manifest Manifest
	Zones    []Zone
}

// ObjectStore is the read-only object-storage contract shared by application
// consumers. S3, MinIO, and an offline test store can all implement it.
type ObjectStore interface {
	Get(context.Context, string) (io.ReadCloser, error)
}

type SnapshotObject struct {
	Key  string
	Size int64
}

type Store interface {
	ObjectStore
	List(context.Context, string) ([]string, error)
	Put(context.Context, string, io.Reader, int64, string) (SnapshotObject, error)
}

// ZoneLoader atomically replaces the target's zone set with the snapshot.
type ZoneLoader interface {
	ReplaceZones(context.Context, []Zone) (int, error)
}

type LoadResult struct {
	SnapshotID string `json:"snapshotId"`
	ZoneCount  int    `json:"zoneCount"`
}

// Write validates and publishes the canonical snapshot representation. The
// content is the uncompressed GeoJSON returned by the producer's database
// export function. Immutable content and manifest objects are written before
// current.json is updated.
func Write(ctx context.Context, store Store, prefix string, content []byte, exportedAt time.Time) (Manifest, error) {
	if store == nil {
		return Manifest{}, errors.New("snapshot object store is required")
	}
	zones, err := parseZones(content)
	if err != nil {
		return Manifest{}, fmt.Errorf("validate zone export: %w", err)
	}
	content = bytes.TrimSpace(content)
	digest := sha256.Sum256(content)
	digestHex := hex.EncodeToString(digest[:])
	exportedAt = exportedAt.UTC()
	snapshotID := exportedAt.Format("20060102T150405Z") + "-" + digestHex[:16]
	prefix = normalizePrefix(prefix)
	base := prefix + "/" + snapshotID
	contentKey := base + ".geojson.gz"
	manifestKey := base + ".manifest.json"
	currentKey := prefix + "/current.json"
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(content); err != nil {
		return Manifest{}, fmt.Errorf("compress zone export: %w", err)
	}
	if err := writer.Close(); err != nil {
		return Manifest{}, fmt.Errorf("finish zone export compression: %w", err)
	}
	if _, err := store.Put(ctx, contentKey, bytes.NewReader(compressed.Bytes()), int64(compressed.Len()), "application/gzip"); err != nil {
		return Manifest{}, fmt.Errorf("upload zone snapshot: %w", err)
	}
	manifest := Manifest{
		SchemaVersion:   SchemaVersion,
		SnapshotID:      snapshotID,
		ExportedAt:      exportedAt.Format(time.RFC3339Nano),
		ZoneCount:       len(zones),
		ContentKey:      contentKey,
		ManifestKey:     manifestKey,
		CurrentKey:      currentKey,
		ContentSHA256:   digestHex,
		ContentBytes:    int64(len(content)),
		CompressedBytes: int64(compressed.Len()),
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Manifest{}, fmt.Errorf("encode zone snapshot manifest: %w", err)
	}
	if _, err := store.Put(ctx, manifestKey, bytes.NewReader(manifestBytes), int64(len(manifestBytes)), "application/json"); err != nil {
		return Manifest{}, fmt.Errorf("upload zone snapshot manifest: %w", err)
	}
	currentBytes, err := json.Marshal(manifest)
	if err != nil {
		return Manifest{}, fmt.Errorf("encode current zone snapshot pointer: %w", err)
	}
	if _, err := store.Put(ctx, currentKey, bytes.NewReader(currentBytes), int64(len(currentBytes)), "application/json"); err != nil {
		return Manifest{}, fmt.Errorf("publish current zone snapshot: %w", err)
	}
	return manifest, nil
}

// Read downloads current.json, the referenced manifest, and the gzip payload;
// it verifies all sizes and the SHA-256 digest before returning any zones.
func Read(ctx context.Context, store ObjectStore, prefix string) (Snapshot, error) {
	if store == nil {
		return Snapshot{}, errors.New("snapshot object store is required")
	}
	prefix = normalizePrefix(prefix)
	currentKey := prefix + "/current.json"
	current, err := readManifest(ctx, store, currentKey)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read current snapshot pointer: %w", err)
	}
	if current.SchemaVersion != SchemaVersion || current.CurrentKey != currentKey {
		return Snapshot{}, errors.New("current snapshot pointer has an unsupported schema")
	}
	if !hasPrefixKey(current.ManifestKey, prefix) || !strings.HasSuffix(current.ManifestKey, ".manifest.json") {
		return Snapshot{}, errors.New("current snapshot pointer has an invalid manifest key")
	}
	manifest, err := readManifest(ctx, store, current.ManifestKey)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read snapshot manifest: %w", err)
	}
	if manifest != current {
		return Snapshot{}, errors.New("current snapshot pointer does not match its manifest")
	}
	if !hasPrefixKey(manifest.ContentKey, prefix) || !strings.HasSuffix(manifest.ContentKey, ".geojson.gz") {
		return Snapshot{}, errors.New("snapshot manifest has an invalid content key")
	}
	content, err := readContent(ctx, store, manifest.ContentKey)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read snapshot content: %w", err)
	}
	if int64(len(content)) != manifest.ContentBytes {
		return Snapshot{}, fmt.Errorf("snapshot content size %d, want %d", len(content), manifest.ContentBytes)
	}
	digest := sha256.Sum256(content)
	if hex.EncodeToString(digest[:]) != strings.ToLower(strings.TrimSpace(manifest.ContentSHA256)) {
		return Snapshot{}, errors.New("snapshot content digest does not match manifest")
	}
	zones, err := parseZones(content)
	if err != nil {
		return Snapshot{}, err
	}
	if len(zones) != manifest.ZoneCount {
		return Snapshot{}, fmt.Errorf("snapshot zone count %d, want %d", len(zones), manifest.ZoneCount)
	}
	return Snapshot{Manifest: manifest, Zones: zones}, nil
}

// Load atomically hands a verified snapshot to the target loader. The
// canonical PostgresLoader below uses api.import_zones, whose SQL function
// performs the replacement in one database transaction.
func Load(ctx context.Context, loader ZoneLoader, snapshot Snapshot) (LoadResult, error) {
	if loader == nil {
		return LoadResult{}, errors.New("zone loader is required")
	}
	if snapshot.Manifest.SchemaVersion != SchemaVersion || snapshot.Manifest.ZoneCount != len(snapshot.Zones) {
		return LoadResult{}, errors.New("snapshot is not verified")
	}
	count, err := loader.ReplaceZones(ctx, snapshot.Zones)
	if err != nil {
		return LoadResult{}, fmt.Errorf("load zone snapshot: %w", err)
	}
	if count != len(snapshot.Zones) {
		return LoadResult{}, fmt.Errorf("zone loader reported %d rows, want %d", count, len(snapshot.Zones))
	}
	return LoadResult{SnapshotID: snapshot.Manifest.SnapshotID, ZoneCount: count}, nil
}

// PostgresLoader implements the shared import contract for the Geo-Zone
// schema. The api.import_zones function owns validation and replacement so the
// operation is atomic and preserves the snapshot's stable UUIDs.
type PostgresLoader struct{ DB *sql.DB }

func (l PostgresLoader) ReplaceZones(ctx context.Context, zones []Zone) (int, error) {
	if l.DB == nil {
		return 0, errors.New("postgres database is required")
	}
	payload, err := json.Marshal(zones)
	if err != nil {
		return 0, fmt.Errorf("encode zones for postgres: %w", err)
	}
	var count int
	if err := l.DB.QueryRowContext(ctx, `SELECT api.import_zones($1::jsonb)`, string(payload)).Scan(&count); err != nil {
		return 0, fmt.Errorf("replace postgres zones: %w", err)
	}
	return count, nil
}

func normalizePrefix(prefix string) string {
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	if prefix == "" {
		return DefaultPrefix
	}
	return prefix
}

func hasPrefixKey(key, prefix string) bool {
	return strings.HasPrefix(key, prefix+"/")
}

func readManifest(ctx context.Context, store ObjectStore, key string) (Manifest, error) {
	payload, err := readObject(ctx, store, key, MaxManifestBytes)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(payload, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	if manifest.SchemaVersion != SchemaVersion || manifest.SnapshotID == "" || manifest.ContentBytes < 0 || manifest.ZoneCount < 0 || manifest.ContentSHA256 == "" {
		return Manifest{}, errors.New("invalid snapshot manifest")
	}
	return manifest, nil
}

func readContent(ctx context.Context, store ObjectStore, key string) ([]byte, error) {
	body, err := store.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	reader, err := gzip.NewReader(body)
	if err != nil {
		return nil, fmt.Errorf("open gzip content: %w", err)
	}
	defer reader.Close()
	content, err := io.ReadAll(io.LimitReader(reader, MaxContentBytes+1))
	if err != nil {
		return nil, fmt.Errorf("decompress content: %w", err)
	}
	if int64(len(content)) > MaxContentBytes {
		return nil, errors.New("snapshot content is too large")
	}
	return content, nil
}

func readObject(ctx context.Context, store ObjectStore, key string, maxBytes int64) ([]byte, error) {
	body, err := store.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	payload, err := io.ReadAll(io.LimitReader(body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read object: %w", err)
	}
	if int64(len(payload)) > maxBytes {
		return nil, errors.New("snapshot object is too large")
	}
	return payload, nil
}

func parseZones(content []byte) ([]Zone, error) {
	var collection struct {
		Type     string            `json:"type"`
		Features []json.RawMessage `json:"features"`
	}
	if err := json.Unmarshal(content, &collection); err != nil {
		return nil, fmt.Errorf("decode GeoJSON: %w", err)
	}
	if collection.Type != "FeatureCollection" || collection.Features == nil {
		return nil, errors.New("snapshot content is not a FeatureCollection")
	}
	if len(collection.Features) > MaxZones {
		return nil, fmt.Errorf("snapshot contains %d zones, maximum is %d", len(collection.Features), MaxZones)
	}
	zones := make([]Zone, 0, len(collection.Features))
	seen := make(map[string]struct{}, len(collection.Features))
	for index, raw := range collection.Features {
		var feature struct {
			Type       string                     `json:"type"`
			ID         string                     `json:"id"`
			Geometry   json.RawMessage            `json:"geometry"`
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(raw, &feature); err != nil {
			return nil, fmt.Errorf("decode feature %d: %w", index, err)
		}
		if feature.Type != "Feature" || strings.TrimSpace(feature.ID) == "" {
			return nil, fmt.Errorf("feature %d has no stable ID", index)
		}
		if _, exists := seen[feature.ID]; exists {
			return nil, fmt.Errorf("duplicate zone ID %q", feature.ID)
		}
		seen[feature.ID] = struct{}{}
		name, err := propertyString(feature.Properties, "name", true)
		if err != nil {
			return nil, fmt.Errorf("feature %d name: %w", index, err)
		}
		description, err := propertyString(feature.Properties, "description", false)
		if err != nil {
			return nil, fmt.Errorf("feature %d description: %w", index, err)
		}
		revision, err := propertyInt(feature.Properties, "revision", DefaultZoneRevision)
		if err != nil {
			return nil, fmt.Errorf("feature %d revision: %w", index, err)
		}
		if revision < 1 || len(feature.Geometry) == 0 || !json.Valid(feature.Geometry) {
			return nil, fmt.Errorf("feature %d has invalid geometry or revision", index)
		}
		var geometry struct {
			Type        string          `json:"type"`
			Coordinates json.RawMessage `json:"coordinates"`
		}
		if err := json.Unmarshal(feature.Geometry, &geometry); err != nil || (geometry.Type != "Polygon" && geometry.Type != "MultiPolygon") || len(geometry.Coordinates) == 0 || string(geometry.Coordinates) == "null" {
			return nil, fmt.Errorf("feature %d has invalid geometry", index)
		}
		zones = append(zones, Zone{ID: feature.ID, Name: name, Description: description, Revision: revision, Geometry: append(json.RawMessage(nil), feature.Geometry...)})
	}
	return zones, nil
}

func propertyString(properties map[string]json.RawMessage, key string, required bool) (string, error) {
	raw, ok := properties[key]
	if !ok {
		if required {
			return "", errors.New("is required")
		}
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", errors.New("must be a string")
	}
	if required && strings.TrimSpace(value) == "" {
		return "", errors.New("is required")
	}
	return value, nil
}

func propertyInt(properties map[string]json.RawMessage, key string, defaultValue int) (int, error) {
	raw, ok := properties[key]
	if !ok {
		return defaultValue, nil
	}
	var value int
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, errors.New("must be an integer")
	}
	return value, nil
}

package configuration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"appliance-code/services/controlplane/internal/blobstore"
)

// MaxArchiveBytes bounds an archive accepted from an untrusted backup target.
// Desired configuration is intentionally small; recovery sets use a separate
// streaming protocol and are not represented by this interface.
const MaxArchiveBytes int64 = 64 << 20

// ErrArchiveNotFound is returned when an archive ID is absent from a target.
var ErrArchiveNotFound = errors.New("configuration: archive not found")

// ArchiveTarget stores the exact sealed archive bytes. It deliberately knows
// nothing about PostgreSQL, appliance identity, or encryption keys, so the
// same archive can move between local disk, appliance object storage, and a
// cloud object store. Callers must call Open before import; target reads alone
// do not establish authenticity.
type ArchiveTarget interface {
	Put(ctx context.Context, manifest ArchiveManifest, data []byte) error
	Get(ctx context.Context, archiveID string) ([]byte, error)
	List(ctx context.Context) ([]ArchiveManifest, error)
}

// FileArchiveTarget stores archives in one dedicated local directory. It is
// suitable for explicit, air-gapped operator export/import staging. It is not
// an off-appliance backup target merely because it is a separate directory.
type FileArchiveTarget struct {
	Root string
}

func (t FileArchiveTarget) Put(ctx context.Context, manifest ArchiveManifest, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateArchiveEnvelope(manifest, data); err != nil {
		return err
	}
	if err := os.MkdirAll(t.Root, 0o700); err != nil {
		return fmt.Errorf("configuration: create archive directory: %w", err)
	}
	temporary, err := os.CreateTemp(t.Root, ".archive-*")
	if err != nil {
		return fmt.Errorf("configuration: create archive staging file: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName) //nolint:errcheck // successful rename removes it
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("configuration: secure archive staging file: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("configuration: write archive: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("configuration: sync archive: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("configuration: close archive: %w", err)
	}
	if err := os.Rename(temporaryName, t.path(manifest.ArchiveID)); err != nil {
		return fmt.Errorf("configuration: publish archive: %w", err)
	}
	return nil
}

func (t FileArchiveTarget) Get(ctx context.Context, archiveID string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateArchiveID(archiveID); err != nil {
		return nil, err
	}
	file, err := os.Open(t.path(archiveID))
	if os.IsNotExist(err) {
		return nil, ErrArchiveNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("configuration: open archive: %w", err)
	}
	defer file.Close() //nolint:errcheck // read result takes precedence
	data, err := io.ReadAll(io.LimitReader(file, MaxArchiveBytes+1))
	if err != nil {
		return nil, fmt.Errorf("configuration: read archive: %w", err)
	}
	if int64(len(data)) > MaxArchiveBytes {
		return nil, fmt.Errorf("configuration: archive exceeds %d byte limit", MaxArchiveBytes)
	}
	return data, nil
}

func (t FileArchiveTarget) List(ctx context.Context) ([]ArchiveManifest, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(t.Root)
	if os.IsNotExist(err) {
		return []ArchiveManifest{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("configuration: list archives: %w", err)
	}
	archives := make([]ArchiveManifest, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		archiveID := strings.TrimSuffix(entry.Name(), ".json")
		if err := validateArchiveID(archiveID); err != nil {
			return nil, fmt.Errorf("configuration: invalid archive target entry %q: %w", entry.Name(), err)
		}
		data, err := t.Get(ctx, archiveID)
		if err != nil {
			return nil, err
		}
		manifest, err := manifestFromArchive(data)
		if err != nil {
			return nil, fmt.Errorf("configuration: inspect archive %q: %w", archiveID, err)
		}
		if manifest.ArchiveID != archiveID {
			return nil, fmt.Errorf("configuration: archive target entry %q has mismatched manifest ID", entry.Name())
		}
		archives = append(archives, manifest)
	}
	sort.Slice(archives, func(i, j int) bool { return archives[i].CreatedAt.After(archives[j].CreatedAt) })
	return archives, nil
}

func (t FileArchiveTarget) path(archiveID string) string {
	return filepath.Join(t.Root, archiveID+".json")
}

// S3ArchiveTarget adapts the existing S3-compatible client to ArchiveTarget.
// Prefix must be a dedicated backup prefix with credentials distinct from
// ordinary appliance content. A target backed by the local appliance blob
// service remains same-appliance convenience storage, not disaster recovery.
type S3ArchiveTarget struct {
	Client *blobstore.Client
	Prefix string
}

func (t S3ArchiveTarget) Put(ctx context.Context, manifest ArchiveManifest, data []byte) error {
	if err := validateArchiveEnvelope(manifest, data); err != nil {
		return err
	}
	if t.Client == nil {
		return fmt.Errorf("configuration: archive S3 client is required")
	}
	_, err := t.Client.Put(ctx, t.key(manifest.ArchiveID), bytes.NewReader(data), int64(len(data)), "application/json")
	if err != nil {
		return fmt.Errorf("configuration: write archive object: %w", err)
	}
	return nil
}

func (t S3ArchiveTarget) Get(ctx context.Context, archiveID string) ([]byte, error) {
	if err := validateArchiveID(archiveID); err != nil {
		return nil, err
	}
	if t.Client == nil {
		return nil, fmt.Errorf("configuration: archive S3 client is required")
	}
	response, object, err := t.Client.Get(ctx, t.key(archiveID), "")
	if errors.Is(err, blobstore.ErrNotFound) {
		return nil, ErrArchiveNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("configuration: read archive object: %w", err)
	}
	defer response.Body.Close() //nolint:errcheck // read result takes precedence
	if object.Size > MaxArchiveBytes {
		return nil, fmt.Errorf("configuration: archive exceeds %d byte limit", MaxArchiveBytes)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, MaxArchiveBytes+1))
	if err != nil {
		return nil, fmt.Errorf("configuration: read archive object body: %w", err)
	}
	if int64(len(data)) > MaxArchiveBytes {
		return nil, fmt.Errorf("configuration: archive exceeds %d byte limit", MaxArchiveBytes)
	}
	return data, nil
}

func (t S3ArchiveTarget) List(ctx context.Context) ([]ArchiveManifest, error) {
	if t.Client == nil {
		return nil, fmt.Errorf("configuration: archive S3 client is required")
	}
	result, err := t.Client.ListAll(ctx, t.prefix())
	if err != nil {
		return nil, fmt.Errorf("configuration: list archive objects: %w", err)
	}
	archives := make([]ArchiveManifest, 0, len(result.Objects))
	for _, object := range result.Objects {
		archiveID, ok := t.archiveIDForKey(object.Key)
		if !ok {
			return nil, fmt.Errorf("configuration: invalid archive object key %q", object.Key)
		}
		data, err := t.Get(ctx, archiveID)
		if err != nil {
			return nil, err
		}
		manifest, err := manifestFromArchive(data)
		if err != nil {
			return nil, fmt.Errorf("configuration: inspect archive object %q: %w", object.Key, err)
		}
		if manifest.ArchiveID != archiveID {
			return nil, fmt.Errorf("configuration: archive object %q has mismatched manifest ID", object.Key)
		}
		archives = append(archives, manifest)
	}
	sort.Slice(archives, func(i, j int) bool { return archives[i].CreatedAt.After(archives[j].CreatedAt) })
	return archives, nil
}

func (t S3ArchiveTarget) prefix() string {
	prefix := strings.Trim(strings.TrimSpace(t.Prefix), "/")
	if prefix == "" {
		return "configuration-archives/"
	}
	return prefix + "/"
}

func (t S3ArchiveTarget) key(archiveID string) string { return t.prefix() + archiveID + ".json" }

func (t S3ArchiveTarget) archiveIDForKey(key string) (string, bool) {
	if !strings.HasPrefix(key, t.prefix()) || !strings.HasSuffix(key, ".json") {
		return "", false
	}
	archiveID := strings.TrimSuffix(strings.TrimPrefix(key, t.prefix()), ".json")
	if strings.Contains(archiveID, "/") || validateArchiveID(archiveID) != nil {
		return "", false
	}
	return archiveID, true
}

func validateArchiveEnvelope(manifest ArchiveManifest, data []byte) error {
	if int64(len(data)) > MaxArchiveBytes {
		return fmt.Errorf("configuration: archive exceeds %d byte limit", MaxArchiveBytes)
	}
	if err := validateArchiveID(manifest.ArchiveID); err != nil {
		return err
	}
	parsed, err := manifestFromArchive(data)
	if err != nil {
		return err
	}
	if parsed != manifest {
		return fmt.Errorf("configuration: archive manifest does not match encrypted envelope")
	}
	return nil
}

func manifestFromArchive(data []byte) (ArchiveManifest, error) {
	var envelope encryptedArchive
	if err := json.Unmarshal(data, &envelope); err != nil {
		return ArchiveManifest{}, fmt.Errorf("configuration: decode archive envelope: %w", err)
	}
	if err := validateArchiveID(envelope.Manifest.ArchiveID); err != nil || envelope.Manifest.Format != ArchiveFormat || envelope.Manifest.SchemaVersion != SchemaVersion || envelope.Manifest.RevisionID == "" || envelope.Manifest.PayloadDigest == "" || envelope.Manifest.KeyID == "" {
		return ArchiveManifest{}, fmt.Errorf("configuration: invalid archive manifest")
	}
	return envelope.Manifest, nil
}

func validateArchiveID(archiveID string) error {
	if !strings.HasPrefix(archiveID, "archive-") || len(archiveID) != len("archive-")+32 {
		return fmt.Errorf("configuration: invalid archive ID")
	}
	for _, r := range strings.TrimPrefix(archiveID, "archive-") {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return fmt.Errorf("configuration: invalid archive ID")
		}
	}
	return nil
}

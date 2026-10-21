package configuration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"testing"
	"time"

	"appliance-code/services/controlplane/internal/storage"
)

type archiveRevisionStore struct{ revision storage.ConfigurationRevision }

func (s archiveRevisionStore) CreateConfigurationRevision(context.Context, storage.ConfigurationRevision) error {
	return nil
}
func (s archiveRevisionStore) GetConfigurationRevision(_ context.Context, id string) (storage.ConfigurationRevision, error) {
	if id != s.revision.ID {
		return storage.ConfigurationRevision{}, storage.ErrNotFound
	}
	return s.revision, nil
}
func (s archiveRevisionStore) LatestConfigurationRevision(context.Context) (storage.ConfigurationRevision, error) {
	return s.revision, nil
}
func (s archiveRevisionStore) SetConfigurationRevisionStatus(context.Context, string, string, string, *time.Time) error {
	return nil
}

type archiveKeyProvider struct{ key []byte }

func (p archiveKeyProvider) ConfigurationArchiveKey(context.Context) ([]byte, string, error) {
	return p.key, "operator-backup-key-v1", nil
}

func TestArchiveServiceExportAndVerify(t *testing.T) {
	payload := []byte(`{"apiVersion":"configuration.zon/v1","customProfiles":[],"profileActivation":{},"dnsRecords":[],"builderCatalog":{"document":"","contentType":"application/json"},"applicationDefinitions":[],"applicationInstances":[]}`)
	sum := sha256.Sum256(payload)
	revision := storage.ConfigurationRevision{
		ID: "revision-1", SchemaVersion: SchemaVersion, Payload: payload,
		PayloadDigest: "sha256:" + hex.EncodeToString(sum[:]), CreatedAt: time.Now().UTC(),
	}
	service := ArchiveService{
		Revisions: archiveRevisionStore{revision: revision},
		Keys:      archiveKeyProvider{key: make([]byte, 32)},
		Target:    FileArchiveTarget{Root: filepath.Join(t.TempDir(), "archives")},
	}
	manifest, err := service.Export(context.Background(), "")
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	verified, err := service.Verify(context.Background(), manifest.ArchiveID)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if verified.ArchiveID != manifest.ArchiveID || verified.PayloadDigest != revision.PayloadDigest {
		t.Fatalf("verified manifest = %#v", verified)
	}
}

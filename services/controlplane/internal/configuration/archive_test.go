package configuration

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"appliance-code/services/controlplane/internal/storage"
)

func TestArchiveSealOpenAndTamperRejection(t *testing.T) {
	payload := []byte(`{"apiVersion":"configuration.zon/v1","customProfiles":[],"profileActivation":{},"dnsRecords":[],"builderCatalog":{"document":"","contentType":"application/json"},"applicationDefinitions":[],"applicationInstances":[]}`)
	sum := sha256.Sum256(payload)
	revision := storage.ConfigurationRevision{ID: "revision-1", SchemaVersion: SchemaVersion, Payload: payload, PayloadDigest: "sha256:" + hex.EncodeToString(sum[:]), CreatedAt: time.Now().UTC()}
	key := bytes.Repeat([]byte{7}, 32)
	data, manifest, err := Seal(revision, key, "backup-key-1")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if manifest.Format != ArchiveFormat || manifest.ArchiveID == "" {
		t.Fatalf("manifest = %#v", manifest)
	}
	gotManifest, gotPayload, err := Open(data, key, "backup-key-1")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if gotManifest.RevisionID != revision.ID || !bytes.Equal(gotPayload, payload) {
		t.Fatalf("opened archive = %#v %q", gotManifest, gotPayload)
	}
	data[len(data)-1] ^= 1
	if _, _, err := Open(data, key, "backup-key-1"); err == nil {
		t.Fatal("tampered archive was accepted")
	}
}

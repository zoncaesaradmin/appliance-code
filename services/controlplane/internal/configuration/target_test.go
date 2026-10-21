package configuration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"appliance-code/services/controlplane/internal/storage"
)

func TestFileArchiveTargetRoundTripAndRejectsTampering(t *testing.T) {
	payload := []byte(`{"apiVersion":"configuration.zon/v1","customProfiles":[],"profileActivation":{},"dnsRecords":[],"builderCatalog":{"document":"","contentType":"application/json"},"applicationDefinitions":[],"applicationInstances":[]}`)
	sum := sha256.Sum256(payload)
	revision := storage.ConfigurationRevision{ID: "revision-1", SchemaVersion: SchemaVersion, Payload: payload, PayloadDigest: "sha256:" + hex.EncodeToString(sum[:]), CreatedAt: time.Now().UTC()}
	data, manifest, err := Seal(revision, bytes.Repeat([]byte{4}, 32), "operator-key-1")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	target := FileArchiveTarget{Root: filepath.Join(t.TempDir(), "archives")}
	if err := target.Put(context.Background(), manifest, data); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := target.Get(context.Background(), manifest.ArchiveID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("stored archive bytes changed")
	}
	archives, err := target.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(archives) != 1 || archives[0].ArchiveID != manifest.ArchiveID {
		t.Fatalf("List = %#v", archives)
	}
	info, err := os.Stat(filepath.Join(target.Root, manifest.ArchiveID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("archive mode = %o, want 600", info.Mode().Perm())
	}

	if err := os.WriteFile(filepath.Join(target.Root, manifest.ArchiveID+".json"), []byte("not-an-archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := target.List(context.Background()); err == nil {
		t.Fatal("List accepted corrupted archive")
	}
}

func TestFileArchiveTargetRejectsPathTraversal(t *testing.T) {
	target := FileArchiveTarget{Root: t.TempDir()}
	if _, err := target.Get(context.Background(), "../outside"); err == nil {
		t.Fatal("Get accepted path traversal ID")
	}
}

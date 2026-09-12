package metadatabundle_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"appliance-code/services/controlplane/internal/metadatabundle"
	"appliance-code/services/controlplane/internal/storage"
	"appliance-code/services/controlplane/internal/storage/sqlite"
	"appliance-code/services/controlplane/internal/version"
)

func TestReleaseServiceDoesNotReuseDevelopmentSourceRecord(t *testing.T) {
	source, err := metadatabundle.DevelopmentDirectory()
	if err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	db, err := sqlite.Open(filepath.Join(dataDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	store := sqlite.NewMetadataBundleStore(db)
	if err := store.PutMetadataBundle(ctx, storage.MetadataBundleRecord{
		Slot: "active", MetadataVersion: "0.0.0.0", SoftwareVersion: "0.0.0-dev",
		DirectoryName: "base", DirectoryPath: source, Signature: "development-only",
	}); err != nil {
		t.Fatal(err)
	}
	old := version.Version
	version.Version = "0.0.0"
	t.Cleanup(func() { version.Version = old })
	if _, err := metadatabundle.NewService(db, store, nil, nil, dataDir); err == nil {
		t.Fatal("release startup must require staged metadata, not reuse development source")
	}
}

func TestDevelopmentLoadsAuthoringTreeWithoutMaterializing(t *testing.T) {
	dir, err := metadatabundle.DevelopmentDirectory()
	if err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	b, err := metadatabundle.LoadStartup(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if b.RootDir != dir {
		t.Fatalf("root = %q, want authoring tree %q", b.RootDir, dir)
	}
	entries, err := os.ReadDir(dataDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("loading metadata must not materialize a copy: %v, %v", entries, err)
	}
}

func TestProductionRequiresStagedMetadataAndIgnoresDevelopmentOverride(t *testing.T) {
	source, err := metadatabundle.DevelopmentDirectory()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("APPLIANCE_DEVELOPMENT_METADATA_DIR", source)
	old := version.Version
	version.Version = "2.4.0"
	t.Cleanup(func() { version.Version = old })
	dataDir := t.TempDir()
	if _, err := metadatabundle.LoadStartup(dataDir); err == nil {
		t.Fatal("production must not fall back to repository metadata")
	}
	if _, err := metadatabundle.LoadDevelopment(); err == nil {
		t.Fatal("production must reject direct repository loading")
	}
	dest := filepath.Join(dataDir, "metadata-bundles", metadatabundle.DirectoryName("2.4.0.0"))
	if err := os.CopyFS(dest, os.DirFS(source)); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(dest, "bundle.yaml")
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	content := strings.ReplaceAll(string(manifest), "0.0.0-dev", "2.4.0")
	content = strings.ReplaceAll(content, "0.0.0.0", "2.4.0.0")
	if err := os.WriteFile(manifestPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := metadatabundle.LoadStartup(dataDir); err != nil {
		t.Fatalf("staged compatible bundle: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte(strings.ReplaceAll(content, "2.4.0", "2.5.0")), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := metadatabundle.LoadStartup(dataDir); err == nil {
		t.Fatal("production must reject incompatible staged metadata")
	}
}

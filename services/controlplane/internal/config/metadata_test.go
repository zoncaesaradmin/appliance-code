package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"appliance-code/services/controlplane/internal/appliance"
	"appliance-code/services/controlplane/internal/config"
	"appliance-code/services/controlplane/internal/metadatabundle"
)

func TestMetadataEditsApplyOnNextStartupWithoutRebuild(t *testing.T) {
	source, err := metadatabundle.DevelopmentDirectory()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(source)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APPLIANCE_DEVELOPMENT_METADATA_DIR", dir)
	first, err := config.Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "profiles", "catalog.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte("  trial:\n    displayName: Trial\n    capabilities: [base, files]\n")...)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	first.ApplianceProfile = "trial"
	if _, err := first.ResolveProfile(); err == nil {
		t.Fatal("running configuration must not hot-reload YAML edits")
	}
	second, err := config.Load([]string{"APPLIANCE_PROFILE=trial"})
	if err != nil {
		t.Fatalf("next startup should see YAML edit without rebuilding: %v", err)
	}
	resolved, err := second.ResolveProfile()
	if err != nil || !resolved.Capabilities.Enabled(appliance.CapabilityFiles) {
		t.Fatalf("new profile was not resolved: %+v, %v", resolved, err)
	}
	if err := os.WriteFile(path, []byte("profiles: [broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(nil); err == nil {
		t.Fatal("invalid YAML must fail the next startup")
	}
	if _, err := second.ResolveProfile(); err != nil {
		t.Fatalf("file changes must not corrupt an existing startup snapshot: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(nil); err == nil {
		t.Fatal("missing catalog must fail startup")
	}
}

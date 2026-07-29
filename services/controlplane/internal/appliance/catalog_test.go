package appliance_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"appliance-code/services/controlplane/internal/appliance"
)

func TestFileCatalogLoaderLoadsBuiltInCatalogDocument(t *testing.T) {
	document := appliance.BuiltInCatalogDocument()
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	path := filepath.Join(t.TempDir(), "appliance-catalog.json")
	if err := os.WriteFile(path, data, 0o640); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}

	loader := appliance.FileCatalogLoader{Path: path}
	resolved, err := appliance.ResolveProfileWithLoader("builder-landns", loader)
	if err != nil {
		t.Fatalf("ResolveProfileWithLoader(builder-landns): %v", err)
	}
	if !resolved.Capabilities.Enabled(appliance.CapabilityBuild) {
		t.Fatal("builder-landns should enable build")
	}
	if !resolved.Capabilities.Enabled(appliance.CapabilityDNS) {
		t.Fatal("builder-landns should enable dns")
	}

	modules, err := appliance.ResolveModulesWithLoader(resolved, appliance.AlwaysEntitled{}, loader)
	if err != nil {
		t.Fatalf("ResolveModulesWithLoader: %v", err)
	}
	for _, moduleName := range []string{
		appliance.ModuleNameHostAgent,
		appliance.ModuleNameArtifactRegistry,
		appliance.ModuleNameBuild,
		appliance.ModuleNameLANDNS,
	} {
		if !appliance.ModuleEnabled(modules, moduleName) {
			t.Fatalf("expected module %q to be enabled", moduleName)
		}
	}
}

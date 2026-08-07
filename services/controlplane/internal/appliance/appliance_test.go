package appliance_test

import (
	"errors"
	"strings"
	"testing"

	"appliance-code/services/controlplane/internal/appliance"
)

func TestResolveProfile(t *testing.T) {
	t.Run("core", func(t *testing.T) {
		resolved, err := appliance.ResolveProfile("core")
		if err != nil {
			t.Fatalf("ResolveProfile(core): %v", err)
		}
		if resolved.Name != appliance.ProfileCore {
			t.Fatalf("resolved.Name = %q, want %q", resolved.Name, appliance.ProfileCore)
		}
		if !resolved.Capabilities.Enabled(appliance.CapabilityBase) {
			t.Fatal("core should enable base")
		}
		if !resolved.Capabilities.Enabled(appliance.CapabilityWorkflows) {
			t.Fatal("core should enable workflows")
		}
		if resolved.Capabilities.Enabled(appliance.CapabilityBuild) {
			t.Fatal("core should not enable build")
		}
		if resolved.Capabilities.Enabled(appliance.CapabilityArtifact) {
			t.Fatal("core should not enable artifact")
		}
	})

	t.Run("builder", func(t *testing.T) {
		resolved, err := appliance.ResolveProfile("builder")
		if err != nil {
			t.Fatalf("ResolveProfile(builder): %v", err)
		}
		for _, capability := range []appliance.Capability{
			appliance.CapabilityBase,
			appliance.CapabilityWorkflows,
			appliance.CapabilityBuild,
			appliance.CapabilityArtifact,
		} {
			if !resolved.Capabilities.Enabled(capability) {
				t.Fatalf("builder should enable %q", capability)
			}
		}
	})

	t.Run("landns", func(t *testing.T) {
		resolved, err := appliance.ResolveProfile("landns")
		if err != nil {
			t.Fatalf("ResolveProfile(landns): %v", err)
		}
		if !resolved.Capabilities.Enabled(appliance.CapabilityBase) {
			t.Fatal("landns should enable base")
		}
		if !resolved.Capabilities.Enabled(appliance.CapabilityDNS) {
			t.Fatal("landns should enable dns")
		}
		if resolved.Capabilities.Enabled(appliance.CapabilityArtifact) {
			t.Fatal("landns should not enable artifact")
		}
		if resolved.Capabilities.Enabled(appliance.CapabilityWorkflows) {
			t.Fatal("landns should not enable workflows")
		}
		if resolved.Capabilities.Enabled(appliance.CapabilityBuild) {
			t.Fatal("landns should not enable build")
		}
	})

	t.Run("storage-landns", func(t *testing.T) {
		resolved, err := appliance.ResolveProfile("storage-landns")
		if err != nil {
			t.Fatalf("ResolveProfile(storage-landns): %v", err)
		}
		for _, capability := range []appliance.Capability{
			appliance.CapabilityBase,
			appliance.CapabilityArtifact,
			appliance.CapabilityDNS,
		} {
			if !resolved.Capabilities.Enabled(capability) {
				t.Fatalf("storage-landns should enable %q", capability)
			}
		}
		if resolved.Capabilities.Enabled(appliance.CapabilityWorkflows) {
			t.Fatal("storage-landns should not enable workflows")
		}
		if resolved.Capabilities.Enabled(appliance.CapabilityBuild) {
			t.Fatal("storage-landns should not enable build")
		}
	})

	for _, name := range []string{"builder-landns", "builder-storage-landns"} {
		t.Run(name, func(t *testing.T) {
			resolved, err := appliance.ResolveProfile(name)
			if err != nil {
				t.Fatalf("ResolveProfile(%s): %v", name, err)
			}
			for _, capability := range []appliance.Capability{
				appliance.CapabilityBase,
				appliance.CapabilityWorkflows,
				appliance.CapabilityBuild,
				appliance.CapabilityArtifact,
				appliance.CapabilityDNS,
			} {
				if !resolved.Capabilities.Enabled(capability) {
					t.Fatalf("%s should enable %q", name, capability)
				}
			}
		})
	}

	t.Run("inference", func(t *testing.T) {
		resolved, err := appliance.ResolveProfile("inference")
		if err != nil {
			t.Fatalf("ResolveProfile(inference): %v", err)
		}
		if !resolved.Capabilities.Enabled(appliance.CapabilityBase) {
			t.Fatal("inference should enable base")
		}
		if !resolved.Capabilities.Enabled(appliance.CapabilityInference) {
			t.Fatal("inference should enable inference")
		}
		if resolved.Capabilities.Enabled(appliance.CapabilityBuild) {
			t.Fatal("inference should not enable build")
		}
		if resolved.Capabilities.Enabled(appliance.CapabilityArtifact) {
			t.Fatal("inference should not enable artifact")
		}
	})

	t.Run("builder-inference", func(t *testing.T) {
		resolved, err := appliance.ResolveProfile("builder-inference")
		if err != nil {
			t.Fatalf("ResolveProfile(builder-inference): %v", err)
		}
		for _, capability := range []appliance.Capability{
			appliance.CapabilityBase,
			appliance.CapabilityWorkflows,
			appliance.CapabilityBuild,
			appliance.CapabilityArtifact,
			appliance.CapabilityInference,
		} {
			if !resolved.Capabilities.Enabled(capability) {
				t.Fatalf("builder-inference should enable %q", capability)
			}
		}
	})

	t.Run("unknown", func(t *testing.T) {
		if _, err := appliance.ResolveProfile("does-not-exist"); err == nil {
			t.Fatal("ResolveProfile should reject an unknown profile")
		}
	})
}

func TestBuiltInProfileCatalogReturnsClone(t *testing.T) {
	catalog := appliance.BuiltInProfileCatalog()
	catalog[appliance.ProfileCore] = appliance.ProfileDefinition{}

	resolved, err := appliance.ResolveProfile("core")
	if err != nil {
		t.Fatalf("ResolveProfile(core): %v", err)
	}
	if !resolved.Capabilities.Enabled(appliance.CapabilityHost) {
		t.Fatal("mutating cloned catalog must not affect built-in core profile")
	}
}

func TestResolveProfileWithLoaderUsesProvidedCatalog(t *testing.T) {
	loader := appliance.StaticProfileCatalogLoader{
		Catalog: appliance.ProfileCatalog{
			"custom": {Capabilities: []appliance.Capability{appliance.CapabilityBase, appliance.CapabilityHost}},
		},
	}
	resolved, err := appliance.ResolveProfileWithLoader("custom", loader)
	if err != nil {
		t.Fatalf("ResolveProfileWithLoader(custom): %v", err)
	}
	if resolved.Name != appliance.Profile("custom") {
		t.Fatalf("resolved.Name = %q, want custom", resolved.Name)
	}
	if !resolved.Capabilities.Enabled(appliance.CapabilityHost) {
		t.Fatal("custom profile should enable host")
	}
}

func TestResolveProfileWithLoaderPropagatesLoaderError(t *testing.T) {
	_, err := appliance.ResolveProfileWithLoader("core", failingProfileCatalogLoader{})
	if err == nil {
		t.Fatal("ResolveProfileWithLoader should fail when the loader fails")
	}
	if !strings.Contains(err.Error(), "load appliance profile catalog") {
		t.Fatalf("error = %q, want loader context", err)
	}
}

type failingProfileCatalogLoader struct{}

func (failingProfileCatalogLoader) LoadProfileCatalog() (appliance.ProfileCatalog, error) {
	return nil, errors.New("boom")
}

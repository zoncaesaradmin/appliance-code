package appliance_test

import (
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

	t.Run("lan-dns", func(t *testing.T) {
		resolved, err := appliance.ResolveProfile("lan-dns")
		if err != nil {
			t.Fatalf("ResolveProfile(lan-dns): %v", err)
		}
		if !resolved.Capabilities.Enabled(appliance.CapabilityBase) {
			t.Fatal("lan-dns should enable base")
		}
		if !resolved.Capabilities.Enabled(appliance.CapabilityDNS) {
			t.Fatal("lan-dns should enable dns")
		}
		if resolved.Capabilities.Enabled(appliance.CapabilityArtifact) {
			t.Fatal("lan-dns should not enable artifact")
		}
		if resolved.Capabilities.Enabled(appliance.CapabilityWorkflows) {
			t.Fatal("lan-dns should not enable workflows")
		}
		if resolved.Capabilities.Enabled(appliance.CapabilityBuild) {
			t.Fatal("lan-dns should not enable build")
		}
	})

	t.Run("storage-lan-dns", func(t *testing.T) {
		resolved, err := appliance.ResolveProfile("storage-lan-dns")
		if err != nil {
			t.Fatalf("ResolveProfile(storage-lan-dns): %v", err)
		}
		for _, capability := range []appliance.Capability{
			appliance.CapabilityBase,
			appliance.CapabilityArtifact,
			appliance.CapabilityDNS,
		} {
			if !resolved.Capabilities.Enabled(capability) {
				t.Fatalf("storage-lan-dns should enable %q", capability)
			}
		}
		if resolved.Capabilities.Enabled(appliance.CapabilityWorkflows) {
			t.Fatal("storage-lan-dns should not enable workflows")
		}
		if resolved.Capabilities.Enabled(appliance.CapabilityBuild) {
			t.Fatal("storage-lan-dns should not enable build")
		}
	})

	t.Run("unknown", func(t *testing.T) {
		if _, err := appliance.ResolveProfile("does-not-exist"); err == nil {
			t.Fatal("ResolveProfile should reject an unknown profile")
		}
	})
}

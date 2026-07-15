package appliance

import (
	"strings"
	"testing"
)

func TestResolveProfileRejectsMissingCapabilityDependency(t *testing.T) {
	const invalidProfile Profile = "invalid-builder-without-artifact"
	original, existed := profileCatalog[invalidProfile]
	profileCatalog[invalidProfile] = []Capability{CapabilityBase, CapabilityWorkflows, CapabilityBuild}
	t.Cleanup(func() {
		if existed {
			profileCatalog[invalidProfile] = original
		} else {
			delete(profileCatalog, invalidProfile)
		}
	})

	_, err := ResolveProfile(string(invalidProfile))
	if err == nil {
		t.Fatal("ResolveProfile should reject build capability without artifact dependency")
	}
	if !strings.Contains(err.Error(), string(CapabilityArtifact)) {
		t.Fatalf("error = %q, want missing artifact dependency", err)
	}
}

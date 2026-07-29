package appliance

import (
	"strings"
	"testing"
)

func TestResolveProfileRejectsMissingCapabilityDependency(t *testing.T) {
	const invalidProfile Profile = "invalid-builder-without-artifact"
	catalog := BuiltInProfileCatalog()
	catalog[invalidProfile] = ProfileDefinition{Capabilities: []Capability{CapabilityBase, CapabilityWorkflows, CapabilityBuild}}

	_, err := ResolveProfileWithCatalog(string(invalidProfile), catalog)
	if err == nil {
		t.Fatal("ResolveProfile should reject build capability without artifact dependency")
	}
	if !strings.Contains(err.Error(), string(CapabilityArtifact)) {
		t.Fatalf("error = %q, want missing artifact dependency", err)
	}
}

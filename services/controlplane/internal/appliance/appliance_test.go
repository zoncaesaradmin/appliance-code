package appliance_test

import (
	"testing"

	"appliance-code/services/controlplane/internal/appliance"
)

func TestResolveProfileUsesCanonicalMetadataCatalog(t *testing.T) {
	tests := []struct {
		name string
		want []appliance.Capability
	}{
		{"core", []appliance.Capability{appliance.CapabilityBase, appliance.CapabilityActions, appliance.CapabilityFiles}},
		{"storage-landns", []appliance.Capability{appliance.CapabilityBase, appliance.CapabilityActions, appliance.CapabilityFiles, appliance.CapabilityArtifact, appliance.CapabilityDNS}},
		{"training", []appliance.Capability{appliance.CapabilityBase, appliance.CapabilityActions, appliance.CapabilityFiles, appliance.CapabilityVideo, appliance.CapabilityPlaintextHTTP}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resolved, err := appliance.ResolveProfile(tc.name)
			if err != nil {
				t.Fatalf("ResolveProfile(%s): %v", tc.name, err)
			}
			for _, capability := range tc.want {
				if !resolved.Capabilities.Enabled(capability) {
					t.Fatalf("%s should enable %q", tc.name, capability)
				}
			}
		})
	}
}

func TestStorageLANDNSDoesNotRequireHost(t *testing.T) {
	resolved, err := appliance.ResolveProfile("storage-landns")
	if err != nil {
		t.Fatal(err)
	}
	for _, capability := range []appliance.Capability{appliance.CapabilityHost} {
		if resolved.Capabilities.Enabled(capability) {
			t.Fatalf("storage-landns unexpectedly enables %q", capability)
		}
	}
	dependencies, ok := appliance.CapabilityDependencies(appliance.CapabilityDNS)
	if !ok || len(dependencies) != 1 || dependencies[0] != appliance.CapabilityBase {
		t.Fatalf("DNS dependencies = %v, want [base]", dependencies)
	}
}

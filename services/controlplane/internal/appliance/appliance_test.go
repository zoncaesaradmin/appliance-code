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
		{"core", []appliance.Capability{appliance.CapabilityBase, appliance.CapabilityFiles}},
		{"builder-storage-landns", []appliance.Capability{appliance.CapabilityBase, appliance.CapabilityHost, appliance.CapabilityFiles, appliance.CapabilityWorkflows, appliance.CapabilityBuild, appliance.CapabilityArtifact, appliance.CapabilityDNS}},
		{"builder-lanllm-storage-landns", []appliance.Capability{appliance.CapabilityBase, appliance.CapabilityHost, appliance.CapabilityFiles, appliance.CapabilityWorkflows, appliance.CapabilityBuild, appliance.CapabilityArtifact, appliance.CapabilityDNS, appliance.CapabilityInference}},
		{"training", []appliance.Capability{appliance.CapabilityBase, appliance.CapabilityHost, appliance.CapabilityFiles, appliance.CapabilityVideo}},
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
	if _, err := appliance.ResolveProfile("builder"); err == nil {
		t.Fatal("removed profile should be rejected")
	}
}

func TestEmbeddedCapabilityCatalogDefinesDependencies(t *testing.T) {
	dependencies, ok := appliance.CapabilityDependencies(appliance.CapabilityBuild)
	if !ok {
		t.Fatal("build capability should be defined by metadata")
	}
	for _, want := range []appliance.Capability{appliance.CapabilityBase, appliance.CapabilityHost, appliance.CapabilityWorkflows, appliance.CapabilityArtifact} {
		found := false
		for _, dependency := range dependencies {
			found = found || dependency == want
		}
		if !found {
			t.Fatalf("build dependencies = %v, missing %q", dependencies, want)
		}
	}
}

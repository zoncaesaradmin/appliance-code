package serviceregistry_test

import (
	"testing"

	"appliance-code/services/controlplane/internal/appliance"
	"appliance-code/services/controlplane/internal/serviceregistry"
)

func TestRegistryFromModulesConvertsHostAgentDescriptor(t *testing.T) {
	resolved, err := appliance.ResolveProfile("training")
	if err != nil {
		t.Fatalf("ResolveProfile(core): %v", err)
	}
	modules, err := appliance.EmbeddedModuleCatalog()
	if err != nil {
		t.Fatalf("EmbeddedModuleCatalog: %v", err)
	}
	modules = appliance.ResolveModules(resolved, appliance.AlwaysEntitled{}, modules)
	registry := serviceregistry.RegistryFromModules(modules)
	if len(registry.Services) != 1 {
		t.Fatalf("len(registry.Services) = %d, want 1", len(registry.Services))
	}
	service := registry.Services[0]
	if service.Name != "host-agent" {
		t.Fatalf("service.Name = %q, want host-agent", service.Name)
	}
	if service.Capability != appliance.CapabilityHost {
		t.Fatalf("service.Capability = %q, want %q", service.Capability, appliance.CapabilityHost)
	}
	if service.BaseURL != "http://host-agent.ace-apps.svc.cluster.local:8080" {
		t.Fatalf("service.BaseURL = %q", service.BaseURL)
	}
	if len(service.Routes) != 11 {
		t.Fatalf("len(service.Routes) = %d, want 11", len(service.Routes))
	}
}

package appliance_test

import (
	"testing"

	"appliance-code/services/controlplane/internal/appliance"
)

func TestResolveModulesIncludesHostAgentWhenHostCapabilityEnabled(t *testing.T) {
	resolved, err := appliance.ResolveProfile("core")
	if err != nil {
		t.Fatalf("ResolveProfile(core): %v", err)
	}
	modules := appliance.ResolveModules(resolved, appliance.AlwaysEntitled{}, appliance.BuiltInModuleCatalog())
	if len(modules) != 2 {
		t.Fatalf("ResolveModules(core) returned %d modules, want 2", len(modules))
	}
	if !appliance.ModuleEnabled(modules, appliance.ModuleNameHostAgent) {
		t.Fatal("core modules should include host-agent")
	}
	if !appliance.ModuleEnabled(modules, appliance.ModuleNameFiles) {
		t.Fatal("core modules should include files")
	}
	module, _ := appliance.ModuleNamed(modules, appliance.ModuleNameHostAgent)
	if module.PrimaryCapability() != appliance.CapabilityHost {
		t.Fatalf("PrimaryCapability = %q, want %q", module.PrimaryCapability(), appliance.CapabilityHost)
	}
	if len(module.Routes) != 7 {
		t.Fatalf("len(module.Routes) = %d, want 7", len(module.Routes))
	}
}

func TestResolveModulesIncludesArtifactAndBuildWhenEnabled(t *testing.T) {
	resolved, err := appliance.ResolveProfile("builder")
	if err != nil {
		t.Fatalf("ResolveProfile(builder): %v", err)
	}
	modules := appliance.ResolveModules(resolved, appliance.AlwaysEntitled{}, appliance.BuiltInModuleCatalog())
	if !appliance.ModuleEnabled(modules, appliance.ModuleNameArtifactRegistry) {
		t.Fatal("builder modules should include artifact-registry")
	}
	if !appliance.ModuleEnabled(modules, appliance.ModuleNameBuild) {
		t.Fatal("builder modules should include build")
	}
}

func TestResolveModulesIncludesDNSWhenEnabled(t *testing.T) {
	resolved, err := appliance.ResolveProfile("landns")
	if err != nil {
		t.Fatalf("ResolveProfile(landns): %v", err)
	}
	modules := appliance.ResolveModules(resolved, appliance.AlwaysEntitled{}, appliance.BuiltInModuleCatalog())
	if !appliance.ModuleEnabled(modules, appliance.ModuleNameLANDNS) {
		t.Fatal("landns modules should include lan-dns")
	}
}

func TestResolveModulesSuppressesModuleWhenNotEntitled(t *testing.T) {
	resolved, err := appliance.ResolveProfile("core")
	if err != nil {
		t.Fatalf("ResolveProfile(core): %v", err)
	}
	modules := appliance.ResolveModules(resolved, denyAllEntitlements{}, appliance.BuiltInModuleCatalog())
	if len(modules) != 0 {
		t.Fatalf("ResolveModules(core) with deny-all entitlements returned %d modules, want 0", len(modules))
	}
}

func TestResolveModulesSkipsHostAgentWithoutHostCapability(t *testing.T) {
	resolved, err := appliance.ResolveProfile("builder")
	if err != nil {
		t.Fatalf("ResolveProfile(builder): %v", err)
	}
	hostless := appliance.ModuleDescriptor{
		Name:                 "host-agent",
		RequiredCapabilities: []appliance.Capability{"missing"},
		BaseURL:              "http://example.invalid",
		Routes:               []appliance.ModuleRoute{{Method: "GET", ExternalPath: "/api/v1/host/info", UpstreamPath: "/internal/v1/host/info", Permission: "host.read"}},
	}
	modules := appliance.ResolveModules(resolved, appliance.AlwaysEntitled{}, []appliance.ModuleDescriptor{hostless})
	if len(modules) != 0 {
		t.Fatalf("ResolveModules(builder) with missing capability returned %d modules, want 0", len(modules))
	}
}

type denyAllEntitlements struct{}

func (denyAllEntitlements) IsEntitled(appliance.ModuleDescriptor, appliance.EntitlementContext) bool {
	return false
}

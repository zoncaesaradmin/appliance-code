package appliance

import (
	"fmt"
	"strings"

	"appliance-code/services/controlplane/internal/metadatabundle"
)

type ModuleKind string

const (
	ModuleKindPlatform    ModuleKind = "platform"
	ModuleKindApplication ModuleKind = "application"
)

const (
	ModuleNameHostAgent        = "host-agent"
	ModuleNameFiles            = "files"
	ModuleNameArtifactRegistry = "artifact-registry"
	ModuleNameLANDNS           = "lan-dns"
	ModuleNameBuild            = "build"
	ModuleNameInferenceRuntime = "inference-runtime"
)

type ExecutionMode string

const (
	ExecutionModeClusterService ExecutionMode = "cluster-service"
	ExecutionModeHostAgent      ExecutionMode = "host-agent"
	ExecutionModeWorkflowBacked ExecutionMode = "workflow-backed"
)

type SecurityClass string

const (
	SecurityClassRestricted     SecurityClass = "restricted"
	SecurityClassHostPrivileged SecurityClass = "host-privileged"
	SecurityClassInternalOnly   SecurityClass = "internal-only"
)

type ModuleRoute struct {
	Method       string
	ExternalPath string
	UpstreamPath string
	Permission   string
}

type ModuleDescriptor struct {
	Name                 string
	Kind                 ModuleKind
	RequiredCapabilities []Capability
	Dependencies         []string
	ExecutionMode        ExecutionMode
	EntitlementKey       string
	BaseURL              string
	Routes               []ModuleRoute
	SecurityClass        SecurityClass
}

func (m ModuleDescriptor) PrimaryCapability() Capability {
	if len(m.RequiredCapabilities) == 0 {
		return ""
	}
	return m.RequiredCapabilities[0]
}

type EntitlementContext struct {
	Profile      ResolvedProfile
	Capabilities Set
}

type EntitlementEvaluator interface {
	IsEntitled(module ModuleDescriptor, ctx EntitlementContext) bool
}

type AlwaysEntitled struct{}

func (AlwaysEntitled) IsEntitled(ModuleDescriptor, EntitlementContext) bool {
	return true
}

func EmbeddedModuleCatalog() ([]ModuleDescriptor, error) {
	catalog, err := metadatabundle.EmbeddedModuleCatalog()
	if err != nil {
		return nil, err
	}
	modules := make([]ModuleDescriptor, 0, len(catalog.Modules))
	for _, module := range catalog.Modules {
		routes := make([]ModuleRoute, len(module.Routes))
		for i, route := range module.Routes {
			routes[i] = ModuleRoute{Method: route.Method, ExternalPath: route.ExternalPath, UpstreamPath: route.UpstreamPath, Permission: route.Permission}
		}
		caps := make([]Capability, len(module.RequiredCapabilities))
		for i, capability := range module.RequiredCapabilities {
			caps[i] = Capability(capability)
		}
		modules = append(modules, ModuleDescriptor{Name: module.Name, Kind: ModuleKind(module.Kind), RequiredCapabilities: caps, Dependencies: append([]string(nil), module.Dependencies...), ExecutionMode: ExecutionMode(module.ExecutionMode), EntitlementKey: module.EntitlementKey, BaseURL: module.BaseURL, Routes: routes, SecurityClass: SecurityClass(module.SecurityClass)})
	}
	if len(modules) == 0 {
		return nil, fmt.Errorf("embedded modules catalog is empty")
	}
	return modules, nil
}

func ResolveModules(resolved ResolvedProfile, evaluator EntitlementEvaluator, catalog []ModuleDescriptor) []ModuleDescriptor {
	return ResolveModulesWithCatalog(resolved, evaluator, catalog)
}

func ResolveModulesWithLoader(resolved ResolvedProfile, evaluator EntitlementEvaluator, loader ModuleCatalogLoader) ([]ModuleDescriptor, error) {
	if evaluator == nil {
		evaluator = AlwaysEntitled{}
	}
	if loader == nil {
		return nil, fmt.Errorf("module catalog loader is required")
	}
	catalog, err := loader.LoadModuleCatalog()
	if err != nil {
		return nil, err
	}
	return ResolveModulesWithCatalog(resolved, evaluator, catalog), nil
}

func ResolveModulesWithCatalog(resolved ResolvedProfile, evaluator EntitlementEvaluator, catalog []ModuleDescriptor) []ModuleDescriptor {
	if evaluator == nil {
		evaluator = AlwaysEntitled{}
	}
	enabled := make([]ModuleDescriptor, 0, len(catalog))
	ctx := EntitlementContext{Profile: resolved, Capabilities: resolved.Capabilities}
	for _, module := range catalog {
		if !moduleEnabled(module, resolved.Capabilities) {
			continue
		}
		if !evaluator.IsEntitled(module, ctx) {
			continue
		}
		enabled = append(enabled, normalizeModule(module))
	}
	return enabled
}

func ModuleNamed(modules []ModuleDescriptor, name string) (ModuleDescriptor, bool) {
	for _, module := range modules {
		if strings.TrimSpace(module.Name) == strings.TrimSpace(name) {
			return module, true
		}
	}
	return ModuleDescriptor{}, false
}

func ModuleEnabled(modules []ModuleDescriptor, name string) bool {
	_, ok := ModuleNamed(modules, name)
	return ok
}

func moduleEnabled(module ModuleDescriptor, capabilities Set) bool {
	for _, capability := range module.RequiredCapabilities {
		if !capabilities.Enabled(capability) {
			return false
		}
	}
	return true
}

func normalizeModule(module ModuleDescriptor) ModuleDescriptor {
	module.Name = strings.TrimSpace(module.Name)
	module.BaseURL = strings.TrimSpace(module.BaseURL)
	for i := range module.Routes {
		module.Routes[i].Method = strings.ToUpper(strings.TrimSpace(module.Routes[i].Method))
		module.Routes[i].ExternalPath = strings.TrimSpace(module.Routes[i].ExternalPath)
		module.Routes[i].UpstreamPath = strings.TrimSpace(module.Routes[i].UpstreamPath)
		module.Routes[i].Permission = strings.TrimSpace(module.Routes[i].Permission)
	}
	return module
}

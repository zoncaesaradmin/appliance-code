package appliance

import (
	"fmt"
	"sort"
	"strings"

	"appliance-code/services/controlplane/internal/metadatabundle"
)

// Profile is the product-facing appliance profile name selected at startup.
type Profile string

const (
	ProfileCore                       Profile = "core"
	ProfileBuilder                    Profile = "builder"
	ProfileStorage                    Profile = "storage"
	ProfileLANDNS                     Profile = "landns"
	ProfileStorageLANDNS              Profile = "storage-landns"
	ProfileBuilderLANDNS              Profile = "builder-landns"
	ProfileBuilderStorageLANDNS       Profile = "builder-storage-landns"
	ProfileLANLLM                     Profile = "lanllm"
	ProfileBuilderLANLLM              Profile = "builder-lanllm"
	ProfileBuilderLANLLMStorageLANDNS Profile = "builder-lanllm-storage-landns"
	ProfileTraining                   Profile = "training"
)

// Capability is the implementation-facing appliance capability name resolved
// from a selected Profile.
type Capability string

const (
	CapabilityBase         Capability = "base"
	CapabilityHost         Capability = "host"
	CapabilityWorkflows    Capability = "workflows"
	CapabilityBuild        Capability = "build"
	CapabilityFiles        Capability = "files"
	CapabilityArtifact     Capability = "artifact"
	CapabilityDNS          Capability = "dns"
	CapabilityInference    Capability = "inference"
	CapabilityVideo        Capability = "video"
	CapabilityApplications Capability = "applications"
)

type capabilityDefinition struct {
	Dependencies []Capability
}

var capabilityCatalog = map[Capability]capabilityDefinition{
	CapabilityBase:         {},
	CapabilityHost:         {Dependencies: []Capability{CapabilityBase}},
	CapabilityWorkflows:    {Dependencies: []Capability{CapabilityBase}},
	CapabilityBuild:        {Dependencies: []Capability{CapabilityBase, CapabilityWorkflows, CapabilityArtifact}},
	CapabilityFiles:        {Dependencies: []Capability{CapabilityBase}},
	CapabilityArtifact:     {Dependencies: []Capability{CapabilityBase}},
	CapabilityDNS:          {Dependencies: []Capability{CapabilityBase}},
	CapabilityInference:    {Dependencies: []Capability{CapabilityBase}},
	CapabilityVideo:        {Dependencies: []Capability{CapabilityBase}},
	CapabilityApplications: {Dependencies: []Capability{CapabilityBase}},
}

type ProfileDefinition struct {
	Capabilities []Capability
}

type ProfileCatalog map[Profile]ProfileDefinition

type ProfileCatalogLoader interface {
	LoadProfileCatalog() (ProfileCatalog, error)
}

type StaticProfileCatalogLoader struct {
	Catalog ProfileCatalog
}

func (l StaticProfileCatalogLoader) LoadProfileCatalog() (ProfileCatalog, error) {
	if l.Catalog == nil {
		return nil, fmt.Errorf("profile catalog loader has no catalog")
	}
	return cloneProfileCatalog(l.Catalog), nil
}

// EmbeddedProfileCatalog converts the one metadata source compiled into this
// image into the resolver's typed catalog. No profile policy is duplicated in
// Go tables or accepted from deployment configuration.
func EmbeddedProfileCatalog() (ProfileCatalog, error) {
	metadataCatalog, err := metadatabundle.EmbeddedProfileCatalog()
	if err != nil {
		return nil, err
	}
	catalog := make(ProfileCatalog, len(metadataCatalog.Profiles))
	for name, definition := range metadataCatalog.Profiles {
		profile := Profile(strings.TrimSpace(name))
		if profile == "" {
			return nil, fmt.Errorf("embedded profiles catalog contains an empty profile name")
		}
		capabilities := make([]Capability, len(definition.Capabilities))
		for i, capability := range definition.Capabilities {
			capabilities[i] = Capability(strings.TrimSpace(capability))
		}
		catalog[profile] = ProfileDefinition{Capabilities: capabilities}
	}
	return catalog, nil
}

// Set is the resolved enabled capability set for one appliance instance.
type Set struct {
	enabled map[Capability]struct{}
}

// Enabled reports whether capability is enabled in the set.
func (s Set) Enabled(capability Capability) bool {
	if s.enabled == nil {
		return false
	}
	_, ok := s.enabled[capability]
	return ok
}

// Names returns the enabled capabilities in stable sorted order.
func (s Set) Names() []Capability {
	names := make([]Capability, 0, len(s.enabled))
	for capability := range s.enabled {
		names = append(names, capability)
	}
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
	return names
}

// ResolvedProfile is the validated appliance profile selected for this
// process, along with its enabled capability set.
type ResolvedProfile struct {
	Name         Profile
	Capabilities Set
}

// ResolveProfile validates name against the v1 appliance-profile catalog and
// returns the resolved enabled capability set. It does not add implicit
// dependencies; invalid profile-to-capability combinations fail closed.
func ResolveProfile(name string) (ResolvedProfile, error) {
	catalog, err := EmbeddedProfileCatalog()
	if err != nil {
		return ResolvedProfile{}, fmt.Errorf("load embedded appliance profile catalog: %w", err)
	}
	return ResolveProfileWithCatalog(name, catalog)
}

func ResolveProfileWithLoader(name string, loader ProfileCatalogLoader) (ResolvedProfile, error) {
	if loader == nil {
		return ResolvedProfile{}, fmt.Errorf("load appliance profile catalog: loader is required")
	}
	catalog, err := loader.LoadProfileCatalog()
	if err != nil {
		return ResolvedProfile{}, fmt.Errorf("load appliance profile catalog: %w", err)
	}
	return ResolveProfileWithCatalog(name, catalog)
}

// ResolveProfile validates name against the provided appliance-profile catalog
// and returns the resolved enabled capability set. It does not add implicit
// dependencies; invalid profile-to-capability combinations fail closed.
func ResolveProfileWithCatalog(name string, catalog ProfileCatalog) (ResolvedProfile, error) {
	profile := Profile(strings.TrimSpace(name))
	definition, ok := catalog[profile]
	if !ok {
		return ResolvedProfile{}, fmt.Errorf("unknown appliance profile %q", name)
	}

	capabilities := definition.Capabilities
	set := Set{enabled: make(map[Capability]struct{}, len(capabilities))}
	for _, capability := range capabilities {
		if _, ok := capabilityCatalog[capability]; !ok {
			return ResolvedProfile{}, fmt.Errorf("appliance profile %q references unknown capability %q", profile, capability)
		}
		set.enabled[capability] = struct{}{}
	}

	if !set.Enabled(CapabilityBase) {
		return ResolvedProfile{}, fmt.Errorf("appliance profile %q must include %q", profile, CapabilityBase)
	}

	for _, capability := range set.Names() {
		def := capabilityCatalog[capability]
		for _, dependency := range def.Dependencies {
			if !set.Enabled(dependency) {
				return ResolvedProfile{}, fmt.Errorf("appliance profile %q enables %q but is missing dependency %q", profile, capability, dependency)
			}
		}
	}

	return ResolvedProfile{Name: profile, Capabilities: set}, nil
}

// IsKnownCapability reports whether capability is in the published catalog.
func IsKnownCapability(capability Capability) bool {
	_, ok := capabilityCatalog[capability]
	return ok
}

// KnownCapabilities returns every published capability in stable order.
func KnownCapabilities() []Capability {
	names := make([]Capability, 0, len(capabilityCatalog))
	for capability := range capabilityCatalog {
		names = append(names, capability)
	}
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
	return names
}

// CapabilityDependencies returns direct dependencies for a capability.
func CapabilityDependencies(capability Capability) ([]Capability, bool) {
	def, ok := capabilityCatalog[capability]
	if !ok {
		return nil, false
	}
	return append([]Capability(nil), def.Dependencies...), true
}

func cloneProfileCatalog(catalog ProfileCatalog) ProfileCatalog {
	cloned := make(ProfileCatalog, len(catalog))
	for profile, definition := range catalog {
		cloned[profile] = ProfileDefinition{
			Capabilities: append([]Capability(nil), definition.Capabilities...),
		}
	}
	return cloned
}

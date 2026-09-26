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
	CapabilityBase          Capability = "base"
	CapabilityLANDiscovery  Capability = "lan-discovery"
	CapabilityHost          Capability = "host"
	CapabilityWorkflows     Capability = "workflows"
	CapabilityBuild         Capability = "build"
	CapabilityFiles         Capability = "files"
	CapabilityArtifact      Capability = "artifact"
	CapabilityDNS           Capability = "dns"
	CapabilityInference     Capability = "inference"
	CapabilityOpenWebUI     Capability = "open-webui"
	CapabilityVideo         Capability = "video"
	CapabilityGuestAccess   Capability = "guest-access"
	CapabilityFocusContent  Capability = "focus-content"
	CapabilityApplications  Capability = "applications"
	CapabilityPlaintextHTTP Capability = "plaintext-http"
)

type CapabilityCatalog map[Capability]capabilityDefinition

type capabilityDefinition struct {
	Dependencies []Capability
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

// DevelopmentProfileCatalog reads the repository catalog for local tools and tests.
// Production wiring uses ProfileCatalogFromMetadata with its startup snapshot.
func DevelopmentProfileCatalog() (ProfileCatalog, error) {
	b, err := metadatabundle.LoadDevelopment()
	if err != nil {
		return nil, err
	}
	return ProfileCatalogFromMetadata(b.Profiles)
}

func ProfileCatalogFromMetadata(metadataCatalog metadatabundle.ProfileCatalog) (ProfileCatalog, error) {
	catalog := make(ProfileCatalog, len(metadataCatalog.Profiles))
	for name, definition := range metadataCatalog.Profiles {
		profile := Profile(strings.TrimSpace(name))
		if profile == "" {
			return nil, fmt.Errorf("profiles catalog contains an empty profile name")
		}
		capabilities := make([]Capability, len(definition.Capabilities))
		for i, capability := range definition.Capabilities {
			capabilities[i] = Capability(strings.TrimSpace(capability))
		}
		catalog[profile] = ProfileDefinition{Capabilities: capabilities}
	}
	return catalog, nil
}

// DevelopmentCapabilityCatalog converts the canonical metadata capability
// catalog. Capability dependency policy is never authored in Go.
func DevelopmentCapabilityCatalog() (CapabilityCatalog, error) {
	b, err := metadatabundle.LoadDevelopment()
	if err != nil {
		return nil, err
	}
	return CapabilityCatalogFromMetadata(b.Capabilities)
}

func CapabilityCatalogFromMetadata(metadataCatalog metadatabundle.CapabilityCatalog) (CapabilityCatalog, error) {
	catalog := make(CapabilityCatalog, len(metadataCatalog.Capabilities))
	for name, definition := range metadataCatalog.Capabilities {
		capability := Capability(strings.TrimSpace(name))
		if capability == "" {
			return nil, fmt.Errorf("capabilities catalog contains an empty capability name")
		}
		dependencies := make([]Capability, len(definition.Requires))
		for i, dependency := range definition.Requires {
			dependencies[i] = Capability(strings.TrimSpace(dependency))
		}
		catalog[capability] = capabilityDefinition{Dependencies: dependencies}
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
	profiles, err := DevelopmentProfileCatalog()
	if err != nil {
		return ResolvedProfile{}, fmt.Errorf("load development appliance profile catalog: %w", err)
	}
	capabilities, err := DevelopmentCapabilityCatalog()
	if err != nil {
		return ResolvedProfile{}, fmt.Errorf("load development appliance capability catalog: %w", err)
	}
	return ResolveProfileWithCatalogs(name, profiles, capabilities)
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
	capabilities, err := DevelopmentCapabilityCatalog()
	if err != nil {
		return ResolvedProfile{}, fmt.Errorf("load development appliance capability catalog: %w", err)
	}
	return ResolveProfileWithCatalogs(name, catalog, capabilities)
}

// ResolveProfileWithCatalogs validates a profile against the supplied metadata
// profiles and capabilities. This is used by metadata-bundle consumers after
// the bundle signature and schema have been validated.
func ResolveProfileWithCatalogs(name string, catalog ProfileCatalog, capabilityCatalog CapabilityCatalog) (ResolvedProfile, error) {
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
	catalog, err := DevelopmentCapabilityCatalog()
	if err != nil {
		return false
	}
	_, ok := catalog[capability]
	return ok
}

// KnownCapabilities returns every published capability in stable order.
func KnownCapabilities() []Capability {
	catalog, err := DevelopmentCapabilityCatalog()
	if err != nil {
		return nil
	}
	names := make([]Capability, 0, len(catalog))
	for capability := range catalog {
		names = append(names, capability)
	}
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
	return names
}

// CapabilityDependencies returns direct dependencies for a capability.
func CapabilityDependencies(capability Capability) ([]Capability, bool) {
	catalog, err := DevelopmentCapabilityCatalog()
	if err != nil {
		return nil, false
	}
	def, ok := catalog[capability]
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

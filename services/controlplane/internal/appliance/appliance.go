package appliance

import (
	"fmt"
	"sort"
	"strings"
)

// Profile is the product-facing appliance profile name selected at startup.
type Profile string

const (
	ProfileCore    Profile = "core"
	ProfileBuilder Profile = "builder"
	ProfileStorage Profile = "storage"
)

// Capability is the implementation-facing appliance capability name resolved
// from a selected Profile.
type Capability string

const (
	CapabilityBase      Capability = "base"
	CapabilityWorkflows Capability = "workflows"
	CapabilityBuild     Capability = "build"
	CapabilityArtifact  Capability = "artifact"
)

type capabilityDefinition struct {
	Dependencies []Capability
}

var capabilityCatalog = map[Capability]capabilityDefinition{
	CapabilityBase:      {},
	CapabilityWorkflows: {Dependencies: []Capability{CapabilityBase}},
	CapabilityBuild:     {Dependencies: []Capability{CapabilityBase, CapabilityWorkflows, CapabilityArtifact}},
	CapabilityArtifact:  {Dependencies: []Capability{CapabilityBase}},
}

var profileCatalog = map[Profile][]Capability{
	ProfileCore:    {CapabilityBase, CapabilityWorkflows},
	ProfileBuilder: {CapabilityBase, CapabilityWorkflows, CapabilityBuild, CapabilityArtifact},
	ProfileStorage: {CapabilityBase, CapabilityArtifact},
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
	profile := Profile(strings.TrimSpace(name))
	capabilities, ok := profileCatalog[profile]
	if !ok {
		return ResolvedProfile{}, fmt.Errorf("unknown appliance profile %q", name)
	}

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

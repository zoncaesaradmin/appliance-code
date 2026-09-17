package metadatabundle

import (
	"fmt"
	"regexp"
	"strings"
)

var packageIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// validatePackageCatalog ensures every capability has exactly one package in
// this metadata bundle. A different bundle can select a different package;
// profiles do not carry a second runtime selector.
func validatePackageCatalog(packages PackageCatalog, capabilities CapabilityCatalog, profiles ProfileCatalog) error {
	if len(packages.Packages) == 0 {
		return fmt.Errorf("metadatabundle: packages catalog is empty")
	}
	owners := map[string][]string{}
	for id, pkg := range packages.Packages {
		if !packageIDPattern.MatchString(id) {
			return fmt.Errorf("metadatabundle: invalid package id %q", id)
		}
		if strings.TrimSpace(pkg.DisplayName) == "" || strings.TrimSpace(pkg.Description) == "" || len(pkg.Capabilities) == 0 {
			return fmt.Errorf("metadatabundle: package %q must define displayName, description, and capabilities", id)
		}
		seen := map[string]bool{}
		for _, capability := range pkg.Capabilities {
			if _, ok := capabilities.Capabilities[capability]; !ok {
				return fmt.Errorf("metadatabundle: package %q references unknown capability %q", id, capability)
			}
			if seen[capability] {
				return fmt.Errorf("metadatabundle: package %q repeats capability %q", id, capability)
			}
			seen[capability] = true
			// Multiple packages may provide a capability. Release assembly selects
			// exactly one of them for a bundle; profiles only require the capability.
			owners[capability] = append(owners[capability], id)
		}
		if seen["inference"] {
			engine := strings.TrimSpace(pkg.Runtime.InferenceEngine)
			if !packageIDPattern.MatchString(engine) {
				return fmt.Errorf("metadatabundle: package %q must declare a valid inference engine", id)
			}
			// Product categories: standard = Ollama; accelerated = vLLM (GPU).
			// Architecture is a product/build dimension, not a pack ID suffix.
			switch id {
			case "std-llm":
				if engine != "ollama" {
					return fmt.Errorf("metadatabundle: standard inference package %q must use ollama", id)
				}
			case "acc-llm":
				if engine != "vllm" {
					return fmt.Errorf("metadatabundle: accelerated inference package %q must use vllm", id)
				}
			default:
				return fmt.Errorf("metadatabundle: unknown inference package %q (want std-llm or acc-llm)", id)
			}
		}
	}
	for capability := range capabilities.Capabilities {
		if len(owners[capability]) == 0 {
			return fmt.Errorf("metadatabundle: capability %q is not assigned to a delivery package", capability)
		}
	}
	for id, profile := range profiles.Profiles {
		for _, capability := range profile.Capabilities {
			if len(owners[capability]) == 0 {
				return fmt.Errorf("metadatabundle: profile %q references unassigned capability %q", id, capability)
			}
		}
	}
	return nil
}

package metadatabundle

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var allowedTopDirs = map[string]struct{}{
	"profiles":      {},
	"capabilities":  {},
	"activation":    {},
	"ui":            {},
	"notifications": {},
	"mcp-tools":     {},
}

// ValidateBundle checks schema, cross-refs, and directory rules.
func ValidateBundle(b *Bundle) error {
	if b == nil {
		return fmt.Errorf("metadatabundle: nil bundle")
	}
	m := b.Manifest
	if m.APIVersion != APIVersion {
		return fmt.Errorf("metadatabundle: unsupported apiVersion %q", m.APIVersion)
	}
	if m.Kind != Kind {
		return fmt.Errorf("metadatabundle: unsupported kind %q", m.Kind)
	}
	if m.SchemaVersion != SchemaVersion {
		return fmt.Errorf("metadatabundle: unsupported schemaVersion %d", m.SchemaVersion)
	}
	if strings.TrimSpace(m.Metadata.SoftwareVersion) == "" {
		return fmt.Errorf("metadatabundle: softwareVersion is required")
	}
	if strings.TrimSpace(m.Metadata.MetadataVersion) == "" {
		return fmt.Errorf("metadatabundle: metadataVersion is required")
	}
	if err := CompatibleWithSoftware(m.Metadata.SoftwareVersion, m.Metadata.MetadataVersion); err != nil {
		return err
	}
	if b.Profiles.Profiles == nil || len(b.Profiles.Profiles) == 0 {
		return fmt.Errorf("metadatabundle: profiles catalog is empty")
	}
	if b.Capabilities.Capabilities == nil || len(b.Capabilities.Capabilities) == 0 {
		return fmt.Errorf("metadatabundle: capabilities catalog is empty")
	}
	for id, cap := range b.Capabilities.Capabilities {
		for _, dep := range cap.Requires {
			if _, ok := b.Capabilities.Capabilities[dep]; !ok {
				return fmt.Errorf("metadatabundle: capability %q requires unknown %q", id, dep)
			}
		}
		for _, conflict := range cap.Conflicts {
			if _, ok := b.Capabilities.Capabilities[conflict]; !ok {
				return fmt.Errorf("metadatabundle: capability %q conflicts with unknown %q", id, conflict)
			}
		}
	}
	for id, profile := range b.Profiles.Profiles {
		if len(profile.Capabilities) == 0 {
			return fmt.Errorf("metadatabundle: profile %q has no capabilities", id)
		}
		hasBase := false
		enabled := map[string]struct{}{}
		for _, c := range profile.Capabilities {
			def, ok := b.Capabilities.Capabilities[c]
			if !ok {
				return fmt.Errorf("metadatabundle: profile %q references unknown capability %q", id, c)
			}
			enabled[c] = struct{}{}
			if c == "base" {
				hasBase = true
			}
			_ = def
		}
		if !hasBase {
			return fmt.Errorf("metadatabundle: profile %q must include base", id)
		}
		for _, c := range profile.Capabilities {
			def := b.Capabilities.Capabilities[c]
			for _, dep := range def.Requires {
				if _, ok := enabled[dep]; !ok {
					return fmt.Errorf("metadatabundle: profile %q capability %q missing dependency %q", id, c, dep)
				}
			}
			for _, conflict := range def.Conflicts {
				if _, ok := enabled[conflict]; ok {
					return fmt.Errorf("metadatabundle: profile %q has conflicting capabilities %q and %q", id, c, conflict)
				}
			}
		}
	}
	if b.RootDir != "" {
		if err := validateDirectoryLayout(b.RootDir); err != nil {
			return err
		}
	}
	return nil
}

func validateDirectoryLayout(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	seenBundleYAML := false
	for _, e := range entries {
		name := e.Name()
		if name == "bundle.yaml" {
			seenBundleYAML = true
			continue
		}
		if e.IsDir() {
			if _, ok := allowedTopDirs[name]; !ok {
				return fmt.Errorf("metadatabundle: unknown top-level directory %q", name)
			}
			continue
		}
		return fmt.Errorf("metadatabundle: unknown top-level file %q", name)
	}
	if !seenBundleYAML {
		return fmt.Errorf("metadatabundle: bundle.yaml is required")
	}
	for _, req := range []string{
		filepath.Join(root, "profiles", "catalog.yaml"),
		filepath.Join(root, "capabilities", "catalog.yaml"),
	} {
		if _, err := os.Stat(req); err != nil {
			return fmt.Errorf("metadatabundle: missing required file %s", filepath.Base(filepath.Dir(req))+"/"+filepath.Base(req))
		}
	}
	return nil
}

// ValidateForSoftware returns grouped validation for install UX.
func ValidateForSoftware(b *Bundle, softwareVersion string) ValidationResult {
	result := ValidationResult{OK: true}
	schema := ValidationGroup{Name: "schema", OK: true, Message: "Schema is valid"}
	if err := ValidateBundle(b); err != nil {
		schema.OK = false
		schema.Message = "Schema validation failed"
		schema.Errors = []string{err.Error()}
		result.OK = false
	}
	result.Groups = append(result.Groups, schema)

	compat := ValidationGroup{Name: "version_compatibility", OK: true, Message: "Metadata version matches software"}
	if err := CompatibleWithSoftware(softwareVersion, b.Manifest.Metadata.MetadataVersion); err != nil {
		compat.OK = false
		compat.Message = "Metadata version is incompatible"
		compat.Errors = []string{err.Error()}
		result.OK = false
	}
	result.Groups = append(result.Groups, compat)
	return result
}

// ProfileIDs returns sorted profile ids.
func (b *Bundle) ProfileIDs() []string {
	ids := make([]string, 0, len(b.Profiles.Profiles))
	for id := range b.Profiles.Profiles {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

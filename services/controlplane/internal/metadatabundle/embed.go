package metadatabundle

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// embedded is a generated snapshot of repository-root metadata-bundle/base.
// Edit the product files only under metadata-bundle/base/, then run
// scripts/package/sync-embedded-metadata-bundle.sh (also run by make build).
//
//go:embed embedded/*
var embeddedRoot embed.FS

type embedFS = embed.FS

// EmbeddedProfileCatalog reads the profile policy compiled into the control
// plane image. The source is metadata-bundle/base/profiles/catalog.yaml.
func EmbeddedProfileCatalog() (ProfileCatalog, error) {
	data, err := embeddedRoot.ReadFile("embedded/profiles/catalog.yaml")
	if err != nil {
		return ProfileCatalog{}, fmt.Errorf("metadatabundle: read embedded profiles catalog: %w", err)
	}
	var catalog ProfileCatalog
	if err := yaml.Unmarshal(data, &catalog); err != nil {
		return ProfileCatalog{}, fmt.Errorf("metadatabundle: parse embedded profiles catalog: %w", err)
	}
	if len(catalog.Profiles) == 0 {
		return ProfileCatalog{}, fmt.Errorf("metadatabundle: embedded profiles catalog is empty")
	}
	return catalog, nil
}

// EmbeddedCapabilityCatalog reads the canonical capability policy compiled
// from metadata-bundle/base/capabilities/catalog.yaml.
func EmbeddedCapabilityCatalog() (CapabilityCatalog, error) {
	data, err := embeddedRoot.ReadFile("embedded/capabilities/catalog.yaml")
	if err != nil {
		return CapabilityCatalog{}, fmt.Errorf("metadatabundle: read embedded capabilities catalog: %w", err)
	}
	var catalog CapabilityCatalog
	if err := yaml.Unmarshal(data, &catalog); err != nil {
		return CapabilityCatalog{}, fmt.Errorf("metadatabundle: parse embedded capabilities catalog: %w", err)
	}
	if len(catalog.Capabilities) == 0 {
		return CapabilityCatalog{}, fmt.Errorf("metadatabundle: embedded capabilities catalog is empty")
	}
	return catalog, nil
}

// EmbeddedModuleCatalog reads the canonical module descriptors compiled from
// metadata-bundle/base/modules/catalog.yaml.
func EmbeddedModuleCatalog() (ModuleCatalog, error) {
	data, err := embeddedRoot.ReadFile("embedded/modules/catalog.yaml")
	if err != nil {
		return ModuleCatalog{}, fmt.Errorf("metadatabundle: read embedded modules catalog: %w", err)
	}
	var catalog ModuleCatalog
	if err := yaml.Unmarshal(data, &catalog); err != nil {
		return ModuleCatalog{}, fmt.Errorf("metadatabundle: parse embedded modules catalog: %w", err)
	}
	if len(catalog.Modules) == 0 {
		return ModuleCatalog{}, fmt.Errorf("metadatabundle: embedded modules catalog is empty")
	}
	return catalog, nil
}

// EmbeddedApplicationCatalog returns the reviewed application contracts from
// metadata-bundle/base/applications/catalog.yaml without interpreting them.
func EmbeddedApplicationCatalog() ([]byte, error) {
	data, err := embeddedRoot.ReadFile("embedded/applications/catalog.yaml")
	if err != nil {
		return nil, fmt.Errorf("metadatabundle: read embedded applications catalog: %w", err)
	}
	return append([]byte(nil), data...), nil
}

// materializeEmbeddedForDevelopment exists only for local test and developer
// startup using the explicit 0.0.0-dev software identity. Production startup
// always requires the signed tree staged by zonctl.
func materializeEmbeddedForDevelopment(dest, softwareVersion, metadataVersion string) error {
	_ = os.RemoveAll(dest)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	return fs.WalkDir(embeddedRoot, "embedded", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(path, "embedded"), "/")
		if rel == "" {
			return nil
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if filepath.Base(path) == "README.generated.md" {
			return nil
		}
		data, err := embeddedRoot.ReadFile(path)
		if err != nil {
			return err
		}
		if filepath.Base(path) == "bundle.yaml" {
			var manifest Manifest
			if err := yaml.Unmarshal(data, &manifest); err != nil {
				return err
			}
			manifest.Metadata.SoftwareVersion = softwareVersion
			manifest.Metadata.MetadataVersion = metadataVersion
			data, err = yaml.Marshal(manifest)
			if err != nil {
				return err
			}
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

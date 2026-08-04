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

func materializeEmbedded(dest, softwareVersion, metadataVersion string) error {
	_ = os.RemoveAll(dest)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	err := fs.WalkDir(embeddedRoot, "embedded", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(path, "embedded")
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" {
			return nil
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		// Packaging/sync markers must not land in active bundle trees.
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
			sw, err := NormalizeSoftwareVersion(softwareVersion)
			if err != nil {
				return err
			}
			manifest.Metadata.SoftwareVersion = sw
			manifest.Metadata.MetadataVersion = metadataVersion
			data, err = yaml.Marshal(&manifest)
			if err != nil {
				return err
			}
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		return fmt.Errorf("metadatabundle: materialize embedded: %w", err)
	}
	return nil
}

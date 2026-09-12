package metadatabundle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"appliance-code/services/controlplane/internal/version"
)

// DevelopmentDirectory locates the single authoring tree. Only development
// binaries may read unsigned repository metadata.
func DevelopmentDirectory() (string, error) {
	if version.Version != "0.0.0-dev" {
		return "", fmt.Errorf("metadatabundle: repository metadata is development-only")
	}
	if dir := strings.TrimSpace(os.Getenv("APPLIANCE_DEVELOPMENT_METADATA_DIR")); dir != "" {
		return filepath.Abs(dir)
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, "metadata-bundle", "base")
		if _, err := os.Stat(filepath.Join(candidate, "bundle.yaml")); err == nil {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("metadatabundle: metadata-bundle/base not found; run from the repository or set APPLIANCE_DEVELOPMENT_METADATA_DIR")
		}
		dir = parent
	}
}

func LoadDevelopment() (*Bundle, error) {
	dir, err := DevelopmentDirectory()
	if err != nil {
		return nil, err
	}
	return LoadDirectory(dir)
}

// LoadStartup takes a file-backed snapshot. Release builds only read the
// version-matched tree staged from the verified offline bundle by zonctl.
// They never fall back to development files or binary-baked policy.
func LoadStartup(dataDir string) (*Bundle, error) {
	if version.Version == "0.0.0-dev" {
		return LoadDevelopment()
	}
	baseVersion, err := BaseMetadataVersion(version.Version)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(dataDir, "metadata-bundles", DirectoryName(baseVersion))
	b, err := LoadDirectory(dir)
	if err != nil {
		return nil, fmt.Errorf("metadatabundle: signed metadata must be staged at %s: %w", dir, err)
	}
	if b.Manifest.Metadata.MetadataVersion != baseVersion {
		return nil, fmt.Errorf("metadatabundle: staged metadata version %q, want %q", b.Manifest.Metadata.MetadataVersion, baseVersion)
	}
	if err := CompatibleWithSoftware(version.Version, b.Manifest.Metadata.MetadataVersion); err != nil {
		return nil, err
	}
	return b, nil
}

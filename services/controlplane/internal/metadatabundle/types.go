package metadatabundle

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	APIVersion    = "metadata.zon/v1"
	Kind          = "ApplianceMetadataBundle"
	SchemaVersion = 1
)

// Bundle is the parsed appliance metadata bundle directory.
type Bundle struct {
	RootDir      string
	Manifest     Manifest
	Profiles     ProfileCatalog
	Capabilities CapabilityCatalog
}

type Manifest struct {
	APIVersion    string           `yaml:"apiVersion" json:"apiVersion"`
	Kind          string           `yaml:"kind" json:"kind"`
	SchemaVersion int              `yaml:"schemaVersion" json:"schemaVersion"`
	Metadata      ManifestMetadata `yaml:"metadata" json:"metadata"`
	Sections      []string         `yaml:"sections" json:"sections"`
}

type ManifestMetadata struct {
	SoftwareVersion string `yaml:"softwareVersion" json:"softwareVersion"`
	MetadataVersion string `yaml:"metadataVersion" json:"metadataVersion"`
	CreatedAt       string `yaml:"createdAt" json:"createdAt"`
	Vendor          string `yaml:"vendor" json:"vendor"`
}

type ProfileCatalog struct {
	Profiles map[string]ProfileDef `yaml:"profiles" json:"profiles"`
}

type ProfileDef struct {
	DisplayName  string   `yaml:"displayName" json:"displayName"`
	Description  string   `yaml:"description" json:"description"`
	Capabilities []string `yaml:"capabilities" json:"capabilities"`
}

type CapabilityCatalog struct {
	Capabilities map[string]CapabilityDef `yaml:"capabilities" json:"capabilities"`
}

type CapabilityDef struct {
	DisplayName string              `yaml:"displayName" json:"displayName"`
	Requires    []string            `yaml:"requires" json:"requires"`
	Conflicts   []string            `yaml:"conflicts" json:"conflicts"`
	License     CapabilityLicense   `yaml:"license" json:"license"`
	Artifacts   CapabilityArtifacts `yaml:"artifacts" json:"artifacts"`
}

type CapabilityLicense struct {
	RequiredEntitlement string `yaml:"requiredEntitlement" json:"requiredEntitlement"`
}

type CapabilityArtifacts struct {
	Required []string `yaml:"required" json:"required"`
}

// Status is the API-facing active metadata bundle status.
type Status struct {
	SoftwareVersion         string `json:"softwareVersion"`
	ActiveMetadataVersion   string `json:"activeMetadataVersion"`
	ActiveDigest            string `json:"activeDigest,omitempty"`
	PreviousMetadataVersion string `json:"previousMetadataVersion,omitempty"`
	PreviousDigest          string `json:"previousDigest,omitempty"`
	DirectoryName           string `json:"directoryName,omitempty"`
	CanRollback             bool   `json:"canRollback"`
}

// ValidationResult groups metadata-bundle validation checks.
type ValidationResult struct {
	OK     bool              `json:"ok"`
	Groups []ValidationGroup `json:"groups"`
}

type ValidationGroup struct {
	Name    string   `json:"name"`
	OK      bool     `json:"ok"`
	Message string   `json:"message,omitempty"`
	Errors  []string `json:"errors,omitempty"`
}

var metadataVersionPattern = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)\.(\d+)$`)
var softwareVersionPattern = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)(?:[-.].*)?$`)

// NormalizeSoftwareVersion returns X.Y.Z for matching (strips -dev/+meta after patch).
func NormalizeSoftwareVersion(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", fmt.Errorf("empty software version")
	}
	// Dev builds: 0.0.0-dev → 0.0.0
	if m := softwareVersionPattern.FindStringSubmatch(v); m != nil {
		return m[1] + "." + m[2] + "." + m[3], nil
	}
	return "", fmt.Errorf("invalid software version %q", v)
}

// ParseMetadataVersion returns major.minor.patch.revision parts.
func ParseMetadataVersion(v string) (maj, min, patch, rev int, err error) {
	m := metadataVersionPattern.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return 0, 0, 0, 0, fmt.Errorf("invalid metadata version %q (want X.Y.Z.N)", v)
	}
	maj, _ = strconv.Atoi(m[1])
	min, _ = strconv.Atoi(m[2])
	patch, _ = strconv.Atoi(m[3])
	rev, _ = strconv.Atoi(m[4])
	return maj, min, patch, rev, nil
}

// CompatibleWithSoftware reports whether metadataVersion matches softwareVersion exactly
// on the first three segments.
func CompatibleWithSoftware(softwareVersion, metadataVersion string) error {
	sw, err := NormalizeSoftwareVersion(softwareVersion)
	if err != nil {
		return err
	}
	maj, min, patch, _, err := ParseMetadataVersion(metadataVersion)
	if err != nil {
		return err
	}
	want := fmt.Sprintf("%d.%d.%d", maj, min, patch)
	if want != sw {
		return fmt.Errorf("metadata version %q is incompatible with software version %q", metadataVersion, softwareVersion)
	}
	return nil
}

// BaseMetadataVersion returns X.Y.Z.0 for a software version.
func BaseMetadataVersion(softwareVersion string) (string, error) {
	sw, err := NormalizeSoftwareVersion(softwareVersion)
	if err != nil {
		return "", err
	}
	return sw + ".0", nil
}

// DirectoryName returns appliance-metadata-bundle-<metadataVersion>.
func DirectoryName(metadataVersion string) string {
	return "appliance-metadata-bundle-" + strings.TrimSpace(metadataVersion)
}

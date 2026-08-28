package metadatabundle

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"appliance-code/services/controlplane/internal/audit"
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
	Modules      ModuleCatalog
	Applications ApplicationCatalog
	DebugTools   *DebugToolsSection
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
	Packages    []string            `yaml:"packages" json:"packages"`
}

// ModuleCatalog declares the platform module surface exposed by a metadata
// release. It intentionally uses plain strings to keep this package independent
// of the control-plane routing implementation.
type ModuleCatalog struct {
	Modules []ModuleDef `yaml:"modules" json:"modules"`
}

type ModuleDef struct {
	Name                 string        `yaml:"name" json:"name"`
	Kind                 string        `yaml:"kind" json:"kind"`
	RequiredCapabilities []string      `yaml:"requiredCapabilities" json:"requiredCapabilities"`
	Dependencies         []string      `yaml:"dependencies" json:"dependencies"`
	ExecutionMode        string        `yaml:"executionMode" json:"executionMode"`
	EntitlementKey       string        `yaml:"entitlementKey" json:"entitlementKey"`
	BaseURL              string        `yaml:"baseURL" json:"baseURL"`
	Routes               []ModuleRoute `yaml:"routes" json:"routes"`
	SecurityClass        string        `yaml:"securityClass" json:"securityClass"`
}

type ModuleRoute struct {
	Method       string `yaml:"method" json:"method"`
	ExternalPath string `yaml:"externalPath" json:"externalPath"`
	UpstreamPath string `yaml:"upstreamPath" json:"upstreamPath"`
	Permission   string `yaml:"permission" json:"permission"`
}

// ApplicationCatalog is preserved as raw YAML so the application subsystem
// owns its contract schema without creating an import cycle here.
type ApplicationCatalog struct {
	Raw []byte `yaml:"-" json:"-"`
}

type CapabilityLicense struct {
	RequiredEntitlement string `yaml:"requiredEntitlement" json:"requiredEntitlement"`
}

type CapabilityArtifacts struct {
	Required []string `yaml:"required" json:"required"`
}

type DebugToolsSection struct {
	AllowedAPIs map[string]AllowedAPI
	Automations map[string]AutomationDocument
}

type AllowedAPIsFile struct {
	APIVersion string       `yaml:"apiVersion" json:"apiVersion"`
	Kind       string       `yaml:"kind" json:"kind"`
	APIs       []AllowedAPI `yaml:"apis" json:"apis"`
}

type AllowedAPI struct {
	ID       string `yaml:"id" json:"id"`
	Function string `yaml:"function" json:"function"`
	Method   string `yaml:"method" json:"method"`
	Path     string `yaml:"path" json:"path"`
}

type AutomationDocument struct {
	ID               string
	FilePath         string
	Document         AutomationDocumentMeta `yaml:"document" json:"document"`
	Input            AutomationIO           `yaml:"input" json:"input"`
	Output           AutomationIO           `yaml:"output" json:"output"`
	Do               []AutomationStep       `yaml:"do" json:"do"`
	SectionDir       string
	InputSchemaPath  string
	OutputSchemaPath string
}

type AutomationDocumentMeta struct {
	DSL       string `yaml:"dsl" json:"dsl"`
	Namespace string `yaml:"namespace" json:"namespace"`
	Name      string `yaml:"name" json:"name"`
	Version   string `yaml:"version" json:"version"`
	Title     string `yaml:"title" json:"title"`
}

type AutomationIO struct {
	Schema AutomationSchemaRef `yaml:"schema" json:"schema"`
}

type AutomationSchemaRef struct {
	Format   string                      `yaml:"format" json:"format"`
	Resource AutomationSchemaResourceRef `yaml:"resource" json:"resource"`
}

type AutomationSchemaResourceRef struct {
	Endpoint string `yaml:"endpoint" json:"endpoint"`
}

type AutomationStep struct {
	Name string
	Call AutomationCall `json:"call"`
}

type AutomationCall struct {
	Function string         `yaml:"call" json:"call"`
	With     map[string]any `yaml:"with" json:"with"`
}

// Status is the API-facing active metadata bundle status.
type Status struct {
	SoftwareVersion         string `json:"softwareVersion"`
	BaseMetadataVersion     string `json:"baseMetadataVersion,omitempty"`
	ActiveMetadataVersion   string `json:"activeMetadataVersion"`
	ActiveDigest            string `json:"activeDigest,omitempty"`
	PreviousMetadataVersion string `json:"previousMetadataVersion,omitempty"`
	PreviousDigest          string `json:"previousDigest,omitempty"`
	DirectoryName           string `json:"directoryName,omitempty"`
	CanRollback             bool   `json:"canRollback"`
}

type AutomationInvokeResult struct {
	AutomationID    string         `json:"automationId"`
	DocumentVersion string         `json:"documentVersion"`
	MetadataVersion string         `json:"metadataVersion"`
	Output          map[string]any `json:"output"`
}

type Runtime interface {
	Status(ctx context.Context) (Status, error)
	ValidateArchive(ctx context.Context, archivePath, signature string) (ValidationResult, *Bundle, error)
	InstallArchive(ctx context.Context, actor audit.Actor, archivePath, signature string) (Status, ValidationResult, error)
	Rollback(ctx context.Context, actor audit.Actor) (Status, error)
	InvokeAutomation(ctx context.Context, actor audit.Actor, automationID string, input []byte) (AutomationInvokeResult, error)
	ActiveBundle(ctx context.Context) (*Bundle, error)
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

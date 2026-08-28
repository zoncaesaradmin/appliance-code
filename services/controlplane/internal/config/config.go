// Package config loads and validates the control plane's typed configuration
// from environment variables, with an optional JSON file providing defaults
// that environment variables override.
package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"appliance-code/services/controlplane/internal/appliance"
	"appliance-code/services/controlplane/internal/applications"
	"appliance-code/services/controlplane/internal/devflows"
	"appliance-code/services/controlplane/internal/metadatabundle"
	"appliance-code/services/controlplane/internal/serviceregistry"
	"gopkg.in/yaml.v3"
)

// Config is the complete typed configuration surface for the control plane
// process. All fields have safe local-development defaults; production
// deployment layers override them through environment variables.
type Config struct {
	ApplianceProfile string `json:"applianceProfile"`
	ApplianceName    string `json:"applianceName"`
	NodeIPv4         string `json:"nodeIPv4"`
	CanonicalOrigin  string `json:"canonicalOrigin"`
	PublicAddr       string `json:"publicAddr"`
	InternalAddr     string `json:"internalAddr"`
	DataDir          string `json:"dataDir"`

	ApplicationLogPath string `json:"applicationLogPath"`
	LogLevel           string `json:"logLevel"`
	TrustedProxyCount  int    `json:"trustedProxyCount"`
	// ApplicationCatalog is parsed from the bundled metadata catalog. Helm and
	// process environment cannot supply application contracts.
	ApplicationCatalog        applications.Catalog     `json:"-"`
	AutomationRuntimeBaseURL  string                   `json:"automationRuntimeBaseURL"`
	ArtifactServerBaseURL     string                   `json:"artifactServerBaseURL"`
	ArtifactServerAllowFake   bool                     `json:"artifactServerAllowFake"`
	ServiceRegistry           serviceregistry.Registry `json:"serviceRegistry"`
	FilesObjectPrefix         string                   `json:"filesObjectPrefix"`
	FilesTransferTimeout      time.Duration            `json:"filesTransferTimeout"`
	FilesMaxUploadBytes       int64                    `json:"filesMaxUploadBytes"`
	DNSReadyURL               string                   `json:"dnsReadyURL"`
	DNSZoneName               string                   `json:"dnsZoneName"`
	DNSConfigMapNamespace     string                   `json:"dnsConfigMapNamespace"`
	DNSConfigMapName          string                   `json:"dnsConfigMapName"`
	DNSBootstrapHostname      string                   `json:"dnsBootstrapHostname"`
	DNSBootstrapIPv4          string                   `json:"dnsBootstrapIPv4"`
	DNSAllowFakeZoneSync      bool                     `json:"dnsAllowFakeZoneSync"`
	InferenceGatewayBaseURL   string                   `json:"inferenceGatewayBaseURL"`
	BlobStorageEndpoint       string                   `json:"blobStorageEndpoint"`
	BlobStorageBucket         string                   `json:"blobStorageBucket"`
	BlobStorageAccessKey      string                   `json:"blobStorageAccessKey"`
	BlobStorageSecretKey      string                   `json:"blobStorageSecretKey"`
	BlobStorageRegion         string                   `json:"blobStorageRegion"`
	VideoLibraryObjectPrefix  string                   `json:"videoLibraryObjectPrefix"`
	VideoLibraryProjectionDir string                   `json:"videoLibraryProjectionDir"`
	VideoTransferTimeout      time.Duration            `json:"videoTransferTimeout"`
	VideoMaxUploadBytes       int64                    `json:"videoMaxUploadBytes"`

	BuildDefaultDeadline            time.Duration    `json:"buildDefaultDeadline"`
	WorkflowEngine                  string           `json:"workflowEngine"`
	WorkflowInstanceID              string           `json:"workflowInstanceID"`
	WorkflowExecutorServiceAccount  string           `json:"workflowExecutorServiceAccount"`
	BuildCatalog                    devflows.Catalog `json:"buildCatalog"`
	WorkspaceProvisionerImageDigest string           `json:"workspaceProvisionerImageDigest"`
	BuilderImageDigest              string           `json:"builderImageDigest"`
	WorkspaceRootDir                string           `json:"workspaceRootDir"`
	WorkspaceClaimName              string           `json:"workspaceClaimName"`

	ReadHeaderTimeout  time.Duration `json:"readHeaderTimeout"`
	ReadTimeout        time.Duration `json:"readTimeout"`
	WriteTimeout       time.Duration `json:"writeTimeout"`
	IdleTimeout        time.Duration `json:"idleTimeout"`
	ShutdownTimeout    time.Duration `json:"shutdownTimeout"`
	MaxHeaderBytes     int64         `json:"maxHeaderBytes"`
	MaxBodyBytes       int64         `json:"maxBodyBytes"`
	AuditRetentionDays int           `json:"auditRetentionDays"`
}

// Default returns the local-development default configuration.
func Default() Config {
	return Config{
		ApplianceProfile:          string(appliance.ProfileCore),
		CanonicalOrigin:           "http://localhost:8080",
		PublicAddr:                "127.0.0.1:8080",
		InternalAddr:              "127.0.0.1:8081",
		DataDir:                   "./data",
		ApplicationLogPath:        "/data/zon/logs/api-server/application.log",
		LogLevel:                  "info",
		TrustedProxyCount:         0,
		AutomationRuntimeBaseURL:  "",
		ArtifactServerAllowFake:   true,
		FilesObjectPrefix:         "files",
		FilesTransferTimeout:      30 * time.Minute,
		FilesMaxUploadBytes:       20 * 1024 * 1024 * 1024,
		BlobStorageEndpoint:       "http://blob-storage.ace-system.svc.cluster.local:9000",
		BlobStorageBucket:         "appliance",
		BlobStorageAccessKey:      "local-dev",
		BlobStorageSecretKey:      "local-dev-secret",
		BlobStorageRegion:         "us-east-1",
		VideoLibraryObjectPrefix:  "video/library",
		VideoLibraryProjectionDir: "/var/lib/appliance/video-media",
		VideoTransferTimeout:      30 * time.Minute,
		VideoMaxUploadBytes:       20 * 1024 * 1024 * 1024,
		DNSZoneName:               "appliance.internal",
		DNSConfigMapNamespace:     "dns",
		DNSConfigMapName:          "dns-server-config",
		DNSAllowFakeZoneSync:      true,
		ReadHeaderTimeout:         5 * time.Second,
		// Must cover multi-GB /api/v1/files uploads. Keep this aligned with
		// FilesTransferTimeout; handlers also extend the connection deadline
		// via ResponseController (requires middleware Unwrap).
		ReadTimeout: 30 * time.Minute,
		// Login verifies Argon2id (64 MiB) before writing a response; under
		// memory pressure that can exceed a short write deadline and the
		// browser surfaces a generic NetworkError. Keep headroom for auth.
		// Large /api/v1/files downloads also need this aligned with
		// FilesTransferTimeout (ResponseController extends further when Unwrap works).
		WriteTimeout:                   30 * time.Minute,
		IdleTimeout:                    60 * time.Second,
		ShutdownTimeout:                30 * time.Second,
		MaxHeaderBytes:                 16 * 1024,
		MaxBodyBytes:                   1 * 1024 * 1024,
		AuditRetentionDays:             365,
		BuildDefaultDeadline:           30 * time.Minute,
		WorkflowEngine:                 "fake",
		WorkflowInstanceID:             "appliance",
		WorkflowExecutorServiceAccount: "appliance-workflows-executor",
		WorkspaceRootDir:               "/data/zon/workspaces",
		WorkspaceClaimName:             "appliance-workspaces",
	}
}

const envPrefix = "APPLIANCE_"

func Load(environ []string) (Config, error) {
	cfg := Default()
	env := parseEnviron(environ)

	if path, ok := env[envPrefix+"CONFIG_FILE"]; ok && path != "" {
		if err := loadFile(path, &cfg); err != nil {
			return Config{}, fmt.Errorf("config: loading %s: %w", path, err)
		}
	}

	if err := applyEnv(&cfg, env); err != nil {
		return Config{}, fmt.Errorf("config: applying environment: %w", err)
	}
	if err := deriveApplicationCatalog(&cfg); err != nil {
		return Config{}, fmt.Errorf("config: application catalog: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("config: invalid configuration: %w", err)
	}

	return cfg, nil
}

func parseEnviron(environ []string) map[string]string {
	out := make(map[string]string, len(environ))
	for _, kv := range environ {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		out[k] = v
	}
	return out
}

func loadFile(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return err
	}
	cfg.BuildCatalog.Normalize()
	return nil
}

func applyEnv(cfg *Config, env map[string]string) error {
	str := func(key string, dst *string) {
		if v, ok := env[envPrefix+key]; ok {
			*dst = v
		}
	}
	str("PROFILE", &cfg.ApplianceProfile)
	str("NAME", &cfg.ApplianceName)
	str("NODE_IPV4", &cfg.NodeIPv4)
	str("CANONICAL_ORIGIN", &cfg.CanonicalOrigin)
	str("PUBLIC_ADDR", &cfg.PublicAddr)
	str("INTERNAL_ADDR", &cfg.InternalAddr)
	str("DATA_DIR", &cfg.DataDir)
	str("APPLICATION_LOG_PATH", &cfg.ApplicationLogPath)
	str("LOG_LEVEL", &cfg.LogLevel)
	str("AUTOMATION_RUNTIME_BASE_URL", &cfg.AutomationRuntimeBaseURL)
	str("ARTIFACT_SERVER_BASE_URL", &cfg.ArtifactServerBaseURL)
	str("FILES_OBJECT_PREFIX", &cfg.FilesObjectPrefix)
	str("DNS_READY_URL", &cfg.DNSReadyURL)
	str("DNS_ZONE_NAME", &cfg.DNSZoneName)
	str("DNS_CONFIGMAP_NAMESPACE", &cfg.DNSConfigMapNamespace)
	str("DNS_CONFIGMAP_NAME", &cfg.DNSConfigMapName)
	str("DNS_BOOTSTRAP_HOSTNAME", &cfg.DNSBootstrapHostname)
	str("DNS_BOOTSTRAP_IPV4", &cfg.DNSBootstrapIPv4)
	str("INFERENCE_GATEWAY_BASE_URL", &cfg.InferenceGatewayBaseURL)
	str("BLOB_STORAGE_ENDPOINT", &cfg.BlobStorageEndpoint)
	str("BLOB_STORAGE_BUCKET", &cfg.BlobStorageBucket)
	str("BLOB_STORAGE_ACCESS_KEY", &cfg.BlobStorageAccessKey)
	str("BLOB_STORAGE_SECRET_KEY", &cfg.BlobStorageSecretKey)
	str("BLOB_STORAGE_REGION", &cfg.BlobStorageRegion)
	str("VIDEO_LIBRARY_OBJECT_PREFIX", &cfg.VideoLibraryObjectPrefix)
	str("VIDEO_LIBRARY_PROJECTION_DIR", &cfg.VideoLibraryProjectionDir)
	str("WORKFLOW_ENGINE", &cfg.WorkflowEngine)
	str("WORKFLOW_INSTANCE_ID", &cfg.WorkflowInstanceID)
	str("WORKFLOW_EXECUTOR_SERVICE_ACCOUNT", &cfg.WorkflowExecutorServiceAccount)
	str("WORKSPACE_PROVISIONER_IMAGE_DIGEST", &cfg.WorkspaceProvisionerImageDigest)
	str("BUILDER_IMAGE_DIGEST", &cfg.BuilderImageDigest)
	str("WORKSPACE_ROOT_DIR", &cfg.WorkspaceRootDir)
	str("WORKSPACE_CLAIM_NAME", &cfg.WorkspaceClaimName)

	var errs []string

	if v, ok := env[envPrefix+"ARTIFACT_SERVER_ALLOW_FAKE"]; ok {
		parsed, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Sprintf("ARTIFACT_SERVER_ALLOW_FAKE: %v", err))
		} else {
			cfg.ArtifactServerAllowFake = parsed
		}
	}
	if v, ok := env[envPrefix+"DNS_ALLOW_FAKE_ZONE_SYNC"]; ok {
		parsed, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Sprintf("DNS_ALLOW_FAKE_ZONE_SYNC: %v", err))
		} else {
			cfg.DNSAllowFakeZoneSync = parsed
		}
	}

	if v, ok := env[envPrefix+"TRUSTED_PROXY_COUNT"]; ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			errs = append(errs, fmt.Sprintf("TRUSTED_PROXY_COUNT: %v", err))
		} else {
			cfg.TrustedProxyCount = n
		}
	}
	if v, ok := env[envPrefix+"AUDIT_RETENTION_DAYS"]; ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			errs = append(errs, fmt.Sprintf("AUDIT_RETENTION_DAYS: %v", err))
		} else {
			cfg.AuditRetentionDays = n
		}
	}

	durs := []struct {
		key string
		dst *time.Duration
	}{
		{"READ_HEADER_TIMEOUT", &cfg.ReadHeaderTimeout},
		{"READ_TIMEOUT", &cfg.ReadTimeout},
		{"WRITE_TIMEOUT", &cfg.WriteTimeout},
		{"IDLE_TIMEOUT", &cfg.IdleTimeout},
		{"SHUTDOWN_TIMEOUT", &cfg.ShutdownTimeout},
		{"BUILD_DEFAULT_DEADLINE", &cfg.BuildDefaultDeadline},
		{"FILES_TRANSFER_TIMEOUT", &cfg.FilesTransferTimeout},
		{"VIDEO_TRANSFER_TIMEOUT", &cfg.VideoTransferTimeout},
	}
	for _, d := range durs {
		if v, ok := env[envPrefix+d.key]; ok {
			parsed, err := time.ParseDuration(v)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", d.key, err))
				continue
			}
			*d.dst = parsed
		}
	}

	ints := []struct {
		key string
		dst *int64
	}{
		{"MAX_HEADER_BYTES", &cfg.MaxHeaderBytes},
		{"MAX_BODY_BYTES", &cfg.MaxBodyBytes},
		{"FILES_MAX_UPLOAD_BYTES", &cfg.FilesMaxUploadBytes},
		{"VIDEO_MAX_UPLOAD_BYTES", &cfg.VideoMaxUploadBytes},
	}
	for _, i := range ints {
		if v, ok := env[envPrefix+i.key]; ok {
			parsed, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", i.key, err))
				continue
			}
			*i.dst = parsed
		}
	}

	if v, ok := env[envPrefix+"BUILD_CATALOG_JSON"]; ok && strings.TrimSpace(v) != "" {
		if err := json.Unmarshal([]byte(v), &cfg.BuildCatalog); err != nil {
			errs = append(errs, fmt.Sprintf("BUILD_CATALOG_JSON: %v", err))
		} else {
			cfg.BuildCatalog.Normalize()
		}
	}
	if v, ok := env[envPrefix+"SERVICE_REGISTRY_JSON"]; ok && strings.TrimSpace(v) != "" {
		if err := json.Unmarshal([]byte(v), &cfg.ServiceRegistry); err != nil {
			errs = append(errs, fmt.Sprintf("SERVICE_REGISTRY_JSON: %v", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func deriveApplicationCatalog(cfg *Config) error {
	cfg.ApplicationCatalog = applications.Catalog{}
	data, err := metadatabundle.EmbeddedApplicationCatalog()
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(data, &cfg.ApplicationCatalog); err != nil {
		return err
	}
	return nil
}

func (c Config) Validate() error {
	var errs []string

	resolved, profileErr := c.ResolveProfile()
	buildEnabled := false
	filesEnabled := false
	artifactEnabled := false
	dnsEnabled := false
	inferenceEnabled := false
	videoEnabled := false
	if profileErr != nil {
		errs = append(errs, fmt.Sprintf("applianceProfile %q is invalid: %v", c.ApplianceProfile, profileErr))
	} else {
		modules, err := c.ResolveModules(resolved)
		if err != nil {
			errs = append(errs, fmt.Sprintf("applianceCatalog is invalid: %v", err))
		}
		buildEnabled = appliance.ModuleEnabled(modules, appliance.ModuleNameBuild)
		filesEnabled = appliance.ModuleEnabled(modules, appliance.ModuleNameFiles)
		artifactEnabled = appliance.ModuleEnabled(modules, appliance.ModuleNameArtifactRegistry)
		dnsEnabled = appliance.ModuleEnabled(modules, appliance.ModuleNameLANDNS)
		inferenceEnabled = appliance.ModuleEnabled(modules, appliance.ModuleNameInferenceRuntime)
		videoEnabled = resolved.Capabilities.Enabled(appliance.CapabilityVideo)
	}
	if profileErr == nil && buildEnabled {
		if !c.BuildCatalog.Empty() {
			if err := c.BuildCatalog.Validate(); err != nil {
				errs = append(errs, err.Error())
			}
		}
		if strings.TrimSpace(c.WorkspaceRootDir) == "" {
			errs = append(errs, "workspaceRootDir must not be empty when the build capability is enabled")
		} else if !strings.HasPrefix(c.WorkspaceRootDir, "/") {
			errs = append(errs, "workspaceRootDir must be an absolute path")
		}
		if strings.TrimSpace(c.WorkspaceClaimName) == "" {
			errs = append(errs, "workspaceClaimName must not be empty when the build capability is enabled")
		}
		if strings.TrimSpace(c.WorkflowExecutorServiceAccount) == "" {
			errs = append(errs, "workflowExecutorServiceAccount must not be empty when the build capability is enabled")
		}
		if strings.TrimSpace(c.WorkspaceProvisionerImageDigest) == "" {
			errs = append(errs, "workspaceProvisionerImageDigest must not be empty when the build capability is enabled")
		} else if !strings.Contains(c.WorkspaceProvisionerImageDigest, "@sha256:") {
			errs = append(errs, "workspaceProvisionerImageDigest must be digest-pinned")
		}
		// builderImageDigest is operator-supplied (not packaged in the appliance).
		// When set it must be digest-pinned; builds fail at submit if the catalog
		// target cannot resolve a usable builder image.
		if digest := strings.TrimSpace(c.BuilderImageDigest); digest != "" && !strings.Contains(digest, "@sha256:") {
			errs = append(errs, "builderImageDigest must be digest-pinned when set")
		}
	} else if profileErr == nil && !buildEnabled && !c.BuildCatalog.Empty() {
		if err := c.BuildCatalog.Validate(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if profileErr == nil && len(c.ServiceRegistry.Services) > 0 {
		if err := c.ServiceRegistry.Validate(resolved.Capabilities); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if profileErr == nil && artifactEnabled {
		if strings.TrimSpace(c.ArtifactServerBaseURL) == "" {
			if !c.ArtifactServerAllowFake {
				errs = append(errs, "artifactServerBaseURL must not be empty when the artifact capability is enabled in production")
			}
		} else if u, err := url.Parse(c.ArtifactServerBaseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" {
			errs = append(errs, "artifactServerBaseURL must be an absolute http(s) URL with no path")
		}
	}
	if runtimeURL := strings.TrimSpace(c.AutomationRuntimeBaseURL); runtimeURL != "" {
		if u, err := url.Parse(runtimeURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" {
			errs = append(errs, "automationRuntimeBaseURL must be an absolute http(s) URL with no path")
		}
	}
	if profileErr == nil && filesEnabled {
		if u, err := url.Parse(c.BlobStorageEndpoint); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" {
			errs = append(errs, "blobStorageEndpoint must be an absolute http(s) URL with no path when the files capability is enabled")
		}
		if strings.TrimSpace(c.BlobStorageBucket) == "" || strings.TrimSpace(c.BlobStorageAccessKey) == "" || strings.TrimSpace(c.BlobStorageSecretKey) == "" {
			errs = append(errs, "blob storage bucket and credentials must be configured when the files capability is enabled")
		}
		if strings.Trim(strings.TrimSpace(c.FilesObjectPrefix), "/") == "" {
			errs = append(errs, "filesObjectPrefix must not be empty when the files capability is enabled")
		}
		if c.FilesTransferTimeout <= 0 {
			errs = append(errs, "filesTransferTimeout must be positive when the files capability is enabled")
		}
		if c.FilesMaxUploadBytes <= 0 {
			errs = append(errs, "filesMaxUploadBytes must be positive when the files capability is enabled")
		}
	}
	if profileErr == nil && dnsEnabled {
		if strings.TrimSpace(c.DNSReadyURL) == "" {
			errs = append(errs, "dnsReadyURL must not be empty when the dns capability is enabled")
		} else if u, err := url.Parse(c.DNSReadyURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			errs = append(errs, "dnsReadyURL must be an absolute http(s) URL")
		}
		if strings.TrimSpace(c.DNSZoneName) == "" {
			errs = append(errs, "dnsZoneName must not be empty when the dns capability is enabled")
		}
		if strings.TrimSpace(c.DNSConfigMapNamespace) == "" {
			errs = append(errs, "dnsConfigMapNamespace must not be empty when the dns capability is enabled")
		}
		if strings.TrimSpace(c.DNSConfigMapName) == "" {
			errs = append(errs, "dnsConfigMapName must not be empty when the dns capability is enabled")
		}
	}
	if profileErr == nil && inferenceEnabled {
		if strings.TrimSpace(c.InferenceGatewayBaseURL) == "" {
			errs = append(errs, "inferenceGatewayBaseURL must not be empty when the inference capability is enabled")
		} else if u, err := url.Parse(c.InferenceGatewayBaseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" {
			errs = append(errs, "inferenceGatewayBaseURL must be an absolute http(s) URL with no path")
		}
	}
	if profileErr == nil && videoEnabled {
		if u, err := url.Parse(c.BlobStorageEndpoint); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" {
			errs = append(errs, "blobStorageEndpoint must be an absolute http(s) URL with no path when the video capability is enabled")
		}
		if strings.TrimSpace(c.BlobStorageBucket) == "" || strings.TrimSpace(c.BlobStorageAccessKey) == "" || strings.TrimSpace(c.BlobStorageSecretKey) == "" {
			errs = append(errs, "blob storage bucket and credentials must be configured when the video capability is enabled")
		}
		if strings.Trim(strings.TrimSpace(c.VideoLibraryObjectPrefix), "/") == "" {
			errs = append(errs, "videoLibraryObjectPrefix must not be empty when the video capability is enabled")
		}
		projectionDir := strings.TrimSpace(c.VideoLibraryProjectionDir)
		if !filepath.IsAbs(projectionDir) || filepath.Clean(projectionDir) == "/" {
			errs = append(errs, "videoLibraryProjectionDir must be an absolute non-root path when the video capability is enabled")
		}
		if c.VideoTransferTimeout <= 0 {
			errs = append(errs, "videoTransferTimeout must be positive when the video capability is enabled")
		}
		if c.VideoMaxUploadBytes <= 0 {
			errs = append(errs, "videoMaxUploadBytes must be positive when the video capability is enabled")
		}
	}

	if name := strings.TrimSpace(c.ApplianceName); name != "" {
		if strings.Contains(name, ".") || strings.ToLower(name) != name {
			errs = append(errs, "applianceName must be a single lowercase DNS label")
		}
	}
	if zone := strings.TrimSpace(c.DNSZoneName); zone != "" {
		if zone == "local" || strings.HasSuffix(zone, ".local") {
			errs = append(errs, "dnsZoneName must not use .local (reserved for mDNS)")
		}
	}
	if u, err := url.Parse(c.CanonicalOrigin); err != nil || u.Scheme == "" || u.Host == "" || u.Path != "" {
		errs = append(errs, "canonicalOrigin must be an absolute URL with no path, e.g. https://registry1.appliance.internal")
	}

	if c.PublicAddr == "" {
		errs = append(errs, "publicAddr must not be empty")
	}
	if c.InternalAddr == "" {
		errs = append(errs, "internalAddr must not be empty")
	}
	if c.PublicAddr == c.InternalAddr {
		errs = append(errs, "publicAddr and internalAddr must differ")
	}
	if c.DataDir == "" {
		errs = append(errs, "dataDir must not be empty")
	}

	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, `logLevel must be one of "debug", "info", "warn", "error"`)
	}

	if c.TrustedProxyCount < 0 {
		errs = append(errs, "trustedProxyCount must not be negative")
	}
	if c.AuditRetentionDays < 90 || c.AuditRetentionDays > 3650 {
		errs = append(errs, "auditRetentionDays must be between 90 and 3650")
	}

	switch c.WorkflowEngine {
	case "fake", "workflows":
	default:
		errs = append(errs, `workflowEngine must be one of "fake", "workflows"`)
	}

	durations := map[string]time.Duration{
		"readHeaderTimeout":    c.ReadHeaderTimeout,
		"readTimeout":          c.ReadTimeout,
		"writeTimeout":         c.WriteTimeout,
		"idleTimeout":          c.IdleTimeout,
		"shutdownTimeout":      c.ShutdownTimeout,
		"buildDefaultDeadline": c.BuildDefaultDeadline,
	}
	for name, d := range durations {
		if d <= 0 {
			errs = append(errs, fmt.Sprintf("%s must be positive", name))
		}
	}

	if c.MaxHeaderBytes <= 0 {
		errs = append(errs, "maxHeaderBytes must be positive")
	}
	if c.MaxBodyBytes <= 0 {
		errs = append(errs, "maxBodyBytes must be positive")
	}

	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func (c Config) ResolveProfile() (appliance.ResolvedProfile, error) {
	catalog, err := c.profileCatalog()
	if err != nil {
		return appliance.ResolvedProfile{}, err
	}
	return appliance.ResolveProfileWithCatalog(c.ApplianceProfile, catalog)
}

func (c Config) ResolveModules(resolved appliance.ResolvedProfile) ([]appliance.ModuleDescriptor, error) {
	catalog, err := c.moduleCatalog()
	if err != nil {
		return nil, err
	}
	return appliance.ResolveModulesWithCatalog(resolved, appliance.AlwaysEntitled{}, catalog), nil
}

func (c Config) profileCatalog() (appliance.ProfileCatalog, error) {
	return appliance.EmbeddedProfileCatalog()
}

func (c Config) moduleCatalog() ([]appliance.ModuleDescriptor, error) {
	return appliance.EmbeddedModuleCatalog()
}

func (c Config) SQLitePath() string {
	return c.DataDir + "/appliance.db"
}

func (c Config) KeysDir() string {
	return c.DataDir + "/keys"
}

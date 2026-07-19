// Package config loads and validates the control plane's typed configuration
// from environment variables, with an optional JSON file providing defaults
// that environment variables override.
package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"appliance-code/services/controlplane/internal/appliance"
	"appliance-code/services/controlplane/internal/devflows"
)

// Config is the complete typed configuration surface for the control plane
// process. All fields have safe local-development defaults; production
// deployment layers override them through environment variables.
type Config struct {
	ApplianceProfile string `json:"applianceProfile"`
	CanonicalOrigin  string `json:"canonicalOrigin"`
	PublicAddr       string `json:"publicAddr"`
	InternalAddr     string `json:"internalAddr"`
	DataDir          string `json:"dataDir"`

	ApplicationLogPath string `json:"applicationLogPath"`
	LogLevel           string `json:"logLevel"`
	TrustedProxyCount  int    `json:"trustedProxyCount"`
	ZotBaseURL         string `json:"zotBaseURL"`

	BuildDefaultDeadline           time.Duration    `json:"buildDefaultDeadline"`
	WorkflowEngine                 string           `json:"workflowEngine"`
	WorkflowInstanceID             string           `json:"workflowInstanceID"`
	WorkflowExecutorServiceAccount string           `json:"workflowExecutorServiceAccount"`
	BuildCatalog                   devflows.Catalog `json:"buildCatalog"`
	WorkspaceRootDir               string           `json:"workspaceRootDir"`
	WorkspaceClaimName             string           `json:"workspaceClaimName"`

	ReadHeaderTimeout time.Duration `json:"readHeaderTimeout"`
	ReadTimeout       time.Duration `json:"readTimeout"`
	WriteTimeout      time.Duration `json:"writeTimeout"`
	IdleTimeout       time.Duration `json:"idleTimeout"`
	ShutdownTimeout   time.Duration `json:"shutdownTimeout"`
	MaxHeaderBytes    int64         `json:"maxHeaderBytes"`
	MaxBodyBytes      int64         `json:"maxBodyBytes"`
}

// Default returns the local-development default configuration.
func Default() Config {
	return Config{
		ApplianceProfile:               string(appliance.ProfileCore),
		CanonicalOrigin:                "http://localhost:8080",
		PublicAddr:                     "127.0.0.1:8080",
		InternalAddr:                   "127.0.0.1:8081",
		DataDir:                        "./data",
		ApplicationLogPath:             "/var/log/appliance/control-plane/application.log",
		LogLevel:                       "info",
		TrustedProxyCount:              0,
		ReadHeaderTimeout:              5 * time.Second,
		ReadTimeout:                    30 * time.Second,
		WriteTimeout:                   30 * time.Second,
		IdleTimeout:                    60 * time.Second,
		ShutdownTimeout:                30 * time.Second,
		MaxHeaderBytes:                 16 * 1024,
		MaxBodyBytes:                   1 * 1024 * 1024,
		BuildDefaultDeadline:           30 * time.Minute,
		WorkflowEngine:                 "fake",
		WorkflowInstanceID:             "appliance",
		WorkflowExecutorServiceAccount: "appliance-argo-workflows-executor",
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
	return json.Unmarshal(data, cfg)
}

func applyEnv(cfg *Config, env map[string]string) error {
	str := func(key string, dst *string) {
		if v, ok := env[envPrefix+key]; ok {
			*dst = v
		}
	}
	str("PROFILE", &cfg.ApplianceProfile)
	str("CANONICAL_ORIGIN", &cfg.CanonicalOrigin)
	str("PUBLIC_ADDR", &cfg.PublicAddr)
	str("INTERNAL_ADDR", &cfg.InternalAddr)
	str("DATA_DIR", &cfg.DataDir)
	str("APPLICATION_LOG_PATH", &cfg.ApplicationLogPath)
	str("LOG_LEVEL", &cfg.LogLevel)
	str("ZOT_BASE_URL", &cfg.ZotBaseURL)
	str("WORKFLOW_ENGINE", &cfg.WorkflowEngine)
	str("WORKFLOW_INSTANCE_ID", &cfg.WorkflowInstanceID)
	str("WORKFLOW_EXECUTOR_SERVICE_ACCOUNT", &cfg.WorkflowExecutorServiceAccount)
	str("WORKSPACE_ROOT_DIR", &cfg.WorkspaceRootDir)
	str("WORKSPACE_CLAIM_NAME", &cfg.WorkspaceClaimName)

	var errs []string

	if v, ok := env[envPrefix+"TRUSTED_PROXY_COUNT"]; ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			errs = append(errs, fmt.Sprintf("TRUSTED_PROXY_COUNT: %v", err))
		} else {
			cfg.TrustedProxyCount = n
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
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func (c Config) Validate() error {
	var errs []string

	resolved, profileErr := appliance.ResolveProfile(c.ApplianceProfile)
	if profileErr != nil {
		errs = append(errs, fmt.Sprintf("applianceProfile %q is invalid: %v", c.ApplianceProfile, profileErr))
	} else if resolved.Capabilities.Enabled(appliance.CapabilityBuild) {
		if c.BuildCatalog.Empty() {
			errs = append(errs, "buildCatalog must not be empty when the build capability is enabled")
		} else if err := c.BuildCatalog.Validate(); err != nil {
			errs = append(errs, err.Error())
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
	} else if !c.BuildCatalog.Empty() {
		if err := c.BuildCatalog.Validate(); err != nil {
			errs = append(errs, err.Error())
		}
	}

	if u, err := url.Parse(c.CanonicalOrigin); err != nil || u.Scheme == "" || u.Host == "" || u.Path != "" {
		errs = append(errs, "canonicalOrigin must be an absolute URL with no path, e.g. https://appliance.example.internal")
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

	switch c.WorkflowEngine {
	case "fake", "argo":
	default:
		errs = append(errs, `workflowEngine must be one of "fake", "argo"`)
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

func (c Config) SQLitePath() string {
	return c.DataDir + "/appliance.db"
}

func (c Config) KeysDir() string {
	return c.DataDir + "/keys"
}

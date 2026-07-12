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
)

// Config is the complete typed configuration surface for the control plane
// process. All fields have safe local-development defaults; production
// deployment layers override them through environment variables.
type Config struct {
	// ApplianceProfile is the product-level appliance profile selected for
	// this deployment. The control plane resolves it into appliance
	// capabilities at startup.
	ApplianceProfile string `json:"applianceProfile"`

	// Environment selects environment-specific behavior (e.g. "development",
	// "production"). It never selects a different persistence backend in v1.
	Environment string `json:"environment"`

	// CanonicalOrigin is the externally reachable origin (scheme://host[:port])
	// used to derive absolute URLs. It must be an absolute http(s) URL with no
	// path component.
	CanonicalOrigin string `json:"canonicalOrigin"`

	// PublicAddr is the listen address for the public-facing API/MCP surface.
	PublicAddr string `json:"publicAddr"`

	// InternalAddr is the listen address for health, version, and future
	// metrics endpoints. It must not be exposed through public ingress.
	InternalAddr string `json:"internalAddr"`

	// DataDir is the directory holding the SQLite database file and other
	// local durable state.
	DataDir string `json:"dataDir"`

	// LogLevel is one of "debug", "info", "warn", "error". Log output format
	// is fixed JSON via the shared platformkit/logging package, matching the
	// convention used across all other repos.
	LogLevel string `json:"logLevel"`

	// TrustedProxyCount is the number of trusted reverse-proxy hops (e.g.
	// Traefik) whose forwarded headers are honored. Zero means no proxy is
	// trusted and forwarded headers are ignored.
	TrustedProxyCount int `json:"trustedProxyCount"`

	// ZotBaseURL is the internal URL of the zot OCI registry data plane
	// (e.g. "http://zot.appliance-registry.svc.cluster.local:5000"). Empty
	// means no zot instance is available, so the repository/tag/referrer
	// catalog endpoints run against an in-process fake with no data — the
	// real ADR 0008 conformance evidence requires a real zot instance.
	ZotBaseURL string `json:"zotBaseURL"`

	// AllowedGitSourceHosts is the build policy allowlist of Git hosts
	// builds may fetch source from. Empty means no host is allowed,
	// failing closed rather than silently permitting arbitrary sources.
	// This is configuration data, not an environment-tuned process knob.
	AllowedGitSourceHosts []string `json:"allowedGitSourceHosts"`

	// AllowedBuilderImageDigests is the build policy allowlist of
	// approved, pinned builder image references builds may use. Empty
	// means unrestricted builder selection until an explicit policy is
	// configured.
	AllowedBuilderImageDigests []string `json:"allowedBuilderImageDigests"`

	// BuildDefaultDeadline bounds how long a build may run before it is
	// automatically cancelled and marked timed out.
	BuildDefaultDeadline time.Duration `json:"buildDefaultDeadline"`

	// ReadHeaderTimeout, ReadTimeout, WriteTimeout, and IdleTimeout bound the
	// public HTTP server per the plan's default HTTP contract.
	ReadHeaderTimeout time.Duration `json:"readHeaderTimeout"`
	ReadTimeout       time.Duration `json:"readTimeout"`
	WriteTimeout      time.Duration `json:"writeTimeout"`
	IdleTimeout       time.Duration `json:"idleTimeout"`

	// ShutdownTimeout bounds graceful drain on shutdown.
	ShutdownTimeout time.Duration `json:"shutdownTimeout"`

	// MaxHeaderBytes and MaxBodyBytes bound request sizes.
	MaxHeaderBytes int64 `json:"maxHeaderBytes"`
	MaxBodyBytes   int64 `json:"maxBodyBytes"`
}

// Default returns the local-development default configuration.
func Default() Config {
	return Config{
		ApplianceProfile:     string(appliance.ProfileCore),
		Environment:          "development",
		CanonicalOrigin:      "http://localhost:8080",
		PublicAddr:           "127.0.0.1:8080",
		InternalAddr:         "127.0.0.1:8081",
		DataDir:              "./data",
		LogLevel:             "info",
		TrustedProxyCount:    0,
		ReadHeaderTimeout:    5 * time.Second,
		ReadTimeout:          30 * time.Second,
		WriteTimeout:         30 * time.Second,
		IdleTimeout:          60 * time.Second,
		ShutdownTimeout:      30 * time.Second,
		MaxHeaderBytes:       16 * 1024,
		MaxBodyBytes:         1 * 1024 * 1024,
		BuildDefaultDeadline: 30 * time.Minute,
	}
}

// envPrefix namespaces every environment variable this process reads, so it
// never collides with unrelated host environment state.
const envPrefix = "APPLIANCE_"

// Load builds a Config starting from Default, layering in an optional JSON
// file named by APPLIANCE_CONFIG_FILE, then applying environment variable
// overrides, and finally validating the result. It never partially applies an
// invalid configuration: on error the returned Config is the zero value.
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

// splitNonEmpty splits a comma-separated environment value, trimming
// whitespace and dropping empty entries so trailing commas don't produce
// spurious blank allowlist entries.
func splitNonEmpty(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
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
	str("ENVIRONMENT", &cfg.Environment)
	str("CANONICAL_ORIGIN", &cfg.CanonicalOrigin)
	str("PUBLIC_ADDR", &cfg.PublicAddr)
	str("INTERNAL_ADDR", &cfg.InternalAddr)
	str("DATA_DIR", &cfg.DataDir)
	str("LOG_LEVEL", &cfg.LogLevel)
	str("ZOT_BASE_URL", &cfg.ZotBaseURL)

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

	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

// Validate fails closed on any configuration value that would put the server
// in an unsafe or non-functional state.
func (c Config) Validate() error {
	var errs []string

	switch c.Environment {
	case "development", "production":
	default:
		errs = append(errs, `environment must be "development" or "production"`)
	}

	if _, err := appliance.ResolveProfile(c.ApplianceProfile); err != nil {
		errs = append(errs, fmt.Sprintf("applianceProfile %q is invalid: %v", c.ApplianceProfile, err))
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

// SQLitePath is the path to the control-plane database file within DataDir.
func (c Config) SQLitePath() string {
	return c.DataDir + "/appliance.db"
}

// KeysDir is the directory holding purpose-separated signing/digest key
// material within DataDir. See internal/keys.
func (c Config) KeysDir() string {
	return c.DataDir + "/keys"
}

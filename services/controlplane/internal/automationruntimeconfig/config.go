package automationruntimeconfig

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr               string
	DataDir            string
	ApplicationLogPath string
	LogLevel           string

	ReadHeaderTimeout  time.Duration
	ReadTimeout        time.Duration
	WriteTimeout       time.Duration
	IdleTimeout        time.Duration
	ShutdownTimeout    time.Duration
	MaxHeaderBytes     int64
	AuditRetentionDays int
}

func Default() Config {
	return Config{
		Addr:               "127.0.0.1:8082",
		DataDir:            "/var/lib/appliance/data",
		ApplicationLogPath: "/data/zon/logs/automation-runtime/application.log",
		LogLevel:           "info",
		ReadHeaderTimeout:  5 * time.Second,
		ReadTimeout:        30 * time.Minute,
		WriteTimeout:       30 * time.Minute,
		IdleTimeout:        60 * time.Second,
		ShutdownTimeout:    30 * time.Second,
		MaxHeaderBytes:     16 * 1024,
		AuditRetentionDays: 365,
	}
}

func Load(environ []string) (Config, error) {
	cfg := Default()
	env := parseEnviron(environ)
	var errs []string

	str := func(key string, dst *string) {
		if v, ok := env["AUTOMATION_RUNTIME_"+key]; ok {
			*dst = v
		}
	}

	str("ADDR", &cfg.Addr)
	str("DATA_DIR", &cfg.DataDir)
	str("APPLICATION_LOG_PATH", &cfg.ApplicationLogPath)
	str("LOG_LEVEL", &cfg.LogLevel)

	for _, item := range []struct {
		key string
		dst *time.Duration
	}{
		{"READ_HEADER_TIMEOUT", &cfg.ReadHeaderTimeout},
		{"READ_TIMEOUT", &cfg.ReadTimeout},
		{"WRITE_TIMEOUT", &cfg.WriteTimeout},
		{"IDLE_TIMEOUT", &cfg.IdleTimeout},
		{"SHUTDOWN_TIMEOUT", &cfg.ShutdownTimeout},
	} {
		if v, ok := env["AUTOMATION_RUNTIME_"+item.key]; ok {
			parsed, err := time.ParseDuration(v)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", item.key, err))
				continue
			}
			*item.dst = parsed
		}
	}

	if v, ok := env["AUTOMATION_RUNTIME_MAX_HEADER_BYTES"]; ok {
		parsed, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			errs = append(errs, fmt.Sprintf("MAX_HEADER_BYTES: %v", err))
		} else {
			cfg.MaxHeaderBytes = parsed
		}
	}
	if v, ok := env["AUTOMATION_RUNTIME_AUDIT_RETENTION_DAYS"]; ok {
		parsed, err := strconv.Atoi(v)
		if err != nil {
			errs = append(errs, fmt.Sprintf("AUDIT_RETENTION_DAYS: %v", err))
		} else {
			cfg.AuditRetentionDays = parsed
		}
	}

	if len(errs) > 0 {
		return Config{}, errors.New(strings.Join(errs, "; "))
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	var errs []string
	if strings.TrimSpace(c.Addr) == "" {
		errs = append(errs, "addr must not be empty")
	}
	if strings.TrimSpace(c.DataDir) == "" {
		errs = append(errs, "dataDir must not be empty")
	} else if !filepath.IsAbs(c.DataDir) {
		errs = append(errs, "dataDir must be an absolute path")
	}
	if strings.TrimSpace(c.ApplicationLogPath) == "" {
		errs = append(errs, "applicationLogPath must not be empty")
	} else if !filepath.IsAbs(c.ApplicationLogPath) {
		errs = append(errs, "applicationLogPath must be an absolute path")
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, `logLevel must be one of "debug", "info", "warn", "error"`)
	}
	for name, value := range map[string]time.Duration{
		"readHeaderTimeout": c.ReadHeaderTimeout,
		"readTimeout":       c.ReadTimeout,
		"writeTimeout":      c.WriteTimeout,
		"idleTimeout":       c.IdleTimeout,
		"shutdownTimeout":   c.ShutdownTimeout,
	} {
		if value <= 0 {
			errs = append(errs, name+" must be positive")
		}
	}
	if c.MaxHeaderBytes <= 0 {
		errs = append(errs, "maxHeaderBytes must be positive")
	}
	if c.AuditRetentionDays < 90 || c.AuditRetentionDays > 3650 {
		errs = append(errs, "auditRetentionDays must be between 90 and 3650")
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func SQLitePath(dataDir string) string {
	return filepath.Join(dataDir, "appliance.db")
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

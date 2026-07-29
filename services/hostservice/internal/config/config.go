package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Addr               string
	HostRoot           string
	ApplicationLogPath string
}

func Default() Config {
	return Config{
		Addr:               "127.0.0.1:18086",
		HostRoot:           "/",
		ApplicationLogPath: "/data/zon/logs/host-server/application.log",
	}
}

func Load(environ []string) (Config, error) {
	cfg := Default()
	for _, kv := range environ {
		key, value, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		switch key {
		case "HOST_SERVICE_ADDR":
			cfg.Addr = value
		case "HOST_SERVICE_HOST_ROOT":
			cfg.HostRoot = value
		case "HOST_SERVICE_APPLICATION_LOG_PATH":
			cfg.ApplicationLogPath = value
		}
	}
	return cfg, cfg.Validate()
}

func LoadFromEnv() (Config, error) {
	return Load(os.Environ())
}

func (c Config) Validate() error {
	var errs []string
	if strings.TrimSpace(c.Addr) == "" {
		errs = append(errs, "addr must not be empty")
	}
	if strings.TrimSpace(c.HostRoot) == "" {
		errs = append(errs, "hostRoot must not be empty")
	} else if !strings.HasPrefix(c.HostRoot, "/") {
		errs = append(errs, "hostRoot must be an absolute path")
	}
	if strings.TrimSpace(c.ApplicationLogPath) == "" {
		errs = append(errs, "applicationLogPath must not be empty")
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Addr               string
	SocketPath         string
	ApplicationLogPath string
	InternalTokenPath  string
}

func Default() Config {
	return Config{
		Addr:               "127.0.0.1:18086",
		SocketPath:         "/run/zon/host-agent/agent.sock",
		ApplicationLogPath: "/data/zon/logs/host-agent/application.log",
		InternalTokenPath:  "/var/run/appliance-internal-auth/api_token_pepper.key",
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
		case "HOST_AGENT_ADDR":
			cfg.Addr = value
		case "HOST_AGENT_SOCKET_PATH":
			cfg.SocketPath = value
		case "HOST_AGENT_APPLICATION_LOG_PATH":
			cfg.ApplicationLogPath = value
		case "HOST_AGENT_INTERNAL_TOKEN_PATH":
			cfg.InternalTokenPath = value
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
	if strings.TrimSpace(c.SocketPath) == "" {
		errs = append(errs, "socketPath must not be empty")
	} else if !strings.HasPrefix(c.SocketPath, "/") {
		errs = append(errs, "socketPath must be an absolute path")
	}
	if strings.TrimSpace(c.ApplicationLogPath) == "" {
		errs = append(errs, "applicationLogPath must not be empty")
	}
	if strings.TrimSpace(c.InternalTokenPath) == "" || !strings.HasPrefix(c.InternalTokenPath, "/") {
		errs = append(errs, "internalTokenPath must be an absolute path")
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

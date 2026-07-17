package config

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Addr                        string
	ApplicationLogPath          string
	ControlPlaneBaseURL         string
	ControlPlaneInternalBaseURL string
	ControlPlaneTrace           bool
	ApplianceProfile            string
	CookieSecure                bool
	LogLevel                    slog.Level
}

func FromEnv() Config {
	return Config{
		Addr:                        env("APPLIANCE_UI_ADDR", "0.0.0.0:8080"),
		ApplicationLogPath:          env("APPLIANCE_UI_APPLICATION_LOG_PATH", "/var/log/appliance/ui/application.log"),
		ControlPlaneBaseURL:         strings.TrimRight(env("APPLIANCE_CONTROL_PLANE_BASE_URL", "http://appliance-control-plane:8080"), "/"),
		ControlPlaneInternalBaseURL: strings.TrimRight(env("APPLIANCE_CONTROL_PLANE_INTERNAL_BASE_URL", "http://appliance-control-plane-internal:8081"), "/"),
		ControlPlaneTrace:           envBool("APPLIANCE_UI_CONTROL_PLANE_TRACE", true),
		ApplianceProfile:            env("APPLIANCE_PROFILE", "core"),
		CookieSecure:                envBool("APPLIANCE_UI_COOKIE_SECURE", true),
		LogLevel:                    logLevel(env("APPLIANCE_UI_LOG_LEVEL", env("APPLIANCE_LOG_LEVEL", "debug"))),
	}
}

func env(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func envBool(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func logLevel(value string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

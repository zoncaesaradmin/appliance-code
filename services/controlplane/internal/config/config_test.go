package config_test

import (
	"strings"
	"testing"

	"appliance-code/services/controlplane/internal/config"
)

func TestDefaultIsValid(t *testing.T) {
	if err := config.Default().Validate(); err != nil {
		t.Fatalf("Default().Validate() = %v, want nil", err)
	}
}

func TestLoadAppliesEnvironmentOverrides(t *testing.T) {
	environ := []string{
		"APPLIANCE_PROFILE=builder",
		"APPLIANCE_PUBLIC_ADDR=0.0.0.0:9000",
		"APPLIANCE_LOG_LEVEL=debug",
		"APPLIANCE_CANONICAL_ORIGIN=https://appliance.example.internal",
	}
	cfg, err := config.Load(environ)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PublicAddr != "0.0.0.0:9000" {
		t.Errorf("PublicAddr = %q, want 0.0.0.0:9000", cfg.PublicAddr)
	}
	if cfg.ApplianceProfile != "builder" {
		t.Errorf("ApplianceProfile = %q, want builder", cfg.ApplianceProfile)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want debug", cfg.LogLevel)
	}
	if cfg.CanonicalOrigin != "https://appliance.example.internal" {
		t.Errorf("CanonicalOrigin = %q, want https://appliance.example.internal", cfg.CanonicalOrigin)
	}
}

func TestLoadRejectsInvalidOverride(t *testing.T) {
	environ := []string{"APPLIANCE_LOG_LEVEL=verbose"}
	_, err := config.Load(environ)
	if err == nil {
		t.Fatal("Load with invalid log level should fail")
	}
	if !strings.Contains(err.Error(), "logLevel") {
		t.Errorf("error = %v, want it to mention logLevel", err)
	}
}

func TestLoadRejectsSameAddrForBothListeners(t *testing.T) {
	environ := []string{
		"APPLIANCE_PUBLIC_ADDR=127.0.0.1:8080",
		"APPLIANCE_INTERNAL_ADDR=127.0.0.1:8080",
	}
	_, err := config.Load(environ)
	if err == nil {
		t.Fatal("Load with identical public/internal addrs should fail")
	}
}

func TestLoadRejectsMalformedDuration(t *testing.T) {
	environ := []string{"APPLIANCE_SHUTDOWN_TIMEOUT=not-a-duration"}
	_, err := config.Load(environ)
	if err == nil {
		t.Fatal("Load with malformed duration should fail")
	}
}

func TestValidateRejectsCanonicalOriginWithPath(t *testing.T) {
	cfg := config.Default()
	cfg.CanonicalOrigin = "https://appliance.example.internal/some/path"
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate should reject a canonical origin with a path component")
	}
}

func TestLoadRejectsUnknownApplianceProfile(t *testing.T) {
	environ := []string{"APPLIANCE_PROFILE=unknown"}
	_, err := config.Load(environ)
	if err == nil {
		t.Fatal("Load with an unknown appliance profile should fail")
	}
	if !strings.Contains(err.Error(), "applianceProfile") {
		t.Fatalf("error = %v, want applianceProfile mentioned", err)
	}
}

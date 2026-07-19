package config_test

import (
	"strings"
	"testing"

	"appliance-code/services/controlplane/internal/config"
	"appliance-code/services/controlplane/internal/devflows"
)

func TestDefaultIsValid(t *testing.T) {
	if err := config.Default().Validate(); err != nil {
		t.Fatalf("Default().Validate() = %v, want nil", err)
	}
}

func TestLoadAppliesEnvironmentOverrides(t *testing.T) {
	environ := []string{
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
	if cfg.ApplianceProfile != "core" {
		t.Errorf("ApplianceProfile = %q, want core", cfg.ApplianceProfile)
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

func TestBuilderProfileRequiresBuildCatalog(t *testing.T) {
	cfg := config.Default()
	cfg.ApplianceProfile = "builder"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "buildCatalog") {
		t.Fatalf("builder profile without catalog error = %v, want buildCatalog", err)
	}
	cfg.BuildCatalog = testBuildCatalog()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("builder profile with catalog Validate: %v", err)
	}
}

func testBuildCatalog() devflows.Catalog {
	return devflows.Catalog{
		WorkProfiles: []devflows.WorkProfile{{Name: "builder", Description: "Builder workflows", Repos: []devflows.ProfileRepo{{Name: "app", EnabledByDefault: true}}}},
		Repos:        []devflows.Repo{{Name: "app", URL: "https://git.internal.example.com/team/app.git", DefaultRef: "0123456789abcdef0123456789abcdef01234567"}},
		BuildTargets: []devflows.BuildTarget{{Name: "default", Aliases: []string{"app"}, Repo: "app", Execution: devflows.ExecutionRepoScript, ImageRepository: "users/alice/app", ImageTagTemplate: "{commit12}", BuilderImageDigest: "buildah@sha256:approved"}},
	}
}

func TestLoadAppliesBuildCatalogJSON(t *testing.T) {
	jsonCatalog := `{"workProfiles":[{"name":"builder","repos":[{"name":"app","enabledByDefault":true}]}],"repos":[{"name":"app","url":"https://git.internal.example.com/team/app.git","defaultRef":"0123456789abcdef0123456789abcdef01234567"}],"buildTargets":[{"name":"default","aliases":["app"],"repo":"app","execution":"repo_script","imageRepository":"users/alice/app","builderImageDigest":"buildah@sha256:approved"}]}`
	cfg, err := config.Load([]string{
		"APPLIANCE_PROFILE=builder",
		"APPLIANCE_BUILD_CATALOG_JSON=" + jsonCatalog,
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.BuildCatalog.BuildTargets) != 1 {
		t.Fatalf("BuildCatalog targets = %+v, want one", cfg.BuildCatalog.BuildTargets)
	}
}

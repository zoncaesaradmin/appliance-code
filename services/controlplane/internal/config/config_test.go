package config_test

import (
	"strings"
	"testing"

	"appliance-code/services/controlplane/internal/appliance"
	"appliance-code/services/controlplane/internal/config"
	"appliance-code/services/controlplane/internal/devflows"
	"appliance-code/services/controlplane/internal/serviceregistry"
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
		"APPLIANCE_FILES_ROOT_DIR=/srv/appliance/files",
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
	if cfg.FilesRootDir != "/srv/appliance/files" {
		t.Errorf("FilesRootDir = %q, want /srv/appliance/files", cfg.FilesRootDir)
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

func TestBuilderProfileAllowsEmptyBuildCatalogAtStartup(t *testing.T) {
	cfg := config.Default()
	cfg.ApplianceProfile = "builder"
	cfg.ArtifactServerBaseURL = "http://appliance-registry.artifacts.svc.cluster.local:5000"
	cfg.WorkspaceProvisionerImageDigest = "workspace-provisioner@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	cfg.BuilderImageDigest = "buildah@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("builder profile with empty catalog Validate: %v", err)
	}
}

func TestBuilderProfileRejectsInvalidSeedBuildCatalog(t *testing.T) {
	cfg := config.Default()
	cfg.ApplianceProfile = "builder"
	cfg.ArtifactServerBaseURL = "http://appliance-registry.artifacts.svc.cluster.local:5000"
	cfg.WorkspaceProvisionerImageDigest = "workspace-provisioner@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	cfg.BuilderImageDigest = "buildah@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	cfg.BuildCatalog = devflows.Catalog{Repos: []devflows.Repo{{Name: "app"}}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid seeded catalog to fail validation")
	}
}

func TestArtifactProfilesRequireRealArtifactServerInProduction(t *testing.T) {
	for _, profile := range []string{"storage", "builder", "storage-landns", "builder-landns", "builder-storage-landns"} {
		t.Run(profile, func(t *testing.T) {
			cfg := config.Default()
			cfg.ApplianceProfile = profile
			cfg.ArtifactServerAllowFake = false
			switch profile {
			case "builder", "builder-landns", "builder-storage-landns":
				cfg.BuildCatalog = testBuildCatalog()
				cfg.WorkspaceProvisionerImageDigest = "workspace-provisioner@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
				cfg.BuilderImageDigest = "dev-build@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			}
			switch profile {
			case "storage-landns", "builder-landns", "builder-storage-landns":
				cfg.DNSReadyURL = "http://dns-server.dns.svc.cluster.local:8181/ready"
			}
			if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "artifactServerBaseURL") {
				t.Fatalf("Validate without artifact server URL = %v, want artifactServerBaseURL error", err)
			}
			cfg.ArtifactServerBaseURL = "http://appliance-registry.artifacts.svc.cluster.local:5000"
			if err := cfg.Validate(); err != nil {
				t.Fatalf("Validate with real artifact server URL: %v", err)
			}
		})
	}
}

func TestDNSProfilesRequireDNSReadyURL(t *testing.T) {
	for _, profile := range []string{"landns", "storage-landns", "builder-landns", "builder-storage-landns"} {
		t.Run(profile, func(t *testing.T) {
			cfg := config.Default()
			cfg.ApplianceProfile = profile
			cfg.DNSReadyURL = ""
			switch profile {
			case "storage-landns":
				cfg.ArtifactServerAllowFake = true
			case "builder-landns", "builder-storage-landns":
				cfg.ArtifactServerAllowFake = true
				cfg.BuildCatalog = testBuildCatalog()
				cfg.WorkspaceProvisionerImageDigest = "workspace-provisioner@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
				cfg.BuilderImageDigest = "dev-build@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			}
			if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "dnsReadyURL") {
				t.Fatalf("Validate without DNS ready URL = %v, want dnsReadyURL error", err)
			}
			cfg.DNSReadyURL = "http://dns-server.dns.svc.cluster.local:8181/ready"
			if err := cfg.Validate(); err != nil {
				t.Fatalf("Validate with DNS ready URL: %v", err)
			}
		})
	}
}

func TestInferenceProfilesRequireInferenceGatewayBaseURL(t *testing.T) {
	for _, profile := range []string{"lanllm", "builder-lanllm", "builder-lanllm-storage-landns"} {
		t.Run(profile, func(t *testing.T) {
			cfg := config.Default()
			cfg.ApplianceProfile = profile
			cfg.InferenceGatewayBaseURL = ""
			switch profile {
			case "builder-lanllm":
				cfg.ArtifactServerAllowFake = true
				cfg.BuildCatalog = testBuildCatalog()
				cfg.WorkspaceProvisionerImageDigest = "workspace-provisioner@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
				cfg.BuilderImageDigest = "dev-build@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			case "builder-lanllm-storage-landns":
				cfg.ArtifactServerAllowFake = true
				cfg.DNSReadyURL = "http://dns-server.dns.svc.cluster.local:8181/ready"
				cfg.BuildCatalog = testBuildCatalog()
				cfg.WorkspaceProvisionerImageDigest = "workspace-provisioner@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
				cfg.BuilderImageDigest = "dev-build@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			}
			if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "inferenceGatewayBaseURL") {
				t.Fatalf("Validate without inference gateway URL = %v, want inferenceGatewayBaseURL error", err)
			}
			cfg.InferenceGatewayBaseURL = "http://inference-gateway.inference.svc.cluster.local:8080"
			if err := cfg.Validate(); err != nil {
				t.Fatalf("Validate with inference gateway URL: %v", err)
			}
		})
	}
}

func TestTrainingProfileRequiresVideoGatewayBaseURL(t *testing.T) {
	cfg := config.Default()
	cfg.ApplianceProfile = "training"
	cfg.VideoGatewayBaseURL = ""
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "videoGatewayBaseURL") {
		t.Fatalf("Validate without video gateway URL = %v, want videoGatewayBaseURL error", err)
	}
	cfg.VideoGatewayBaseURL = "http://video-gateway.video.svc.cluster.local:8096"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate with video gateway URL: %v", err)
	}
}

func TestArtifactProfileAllowsExplicitFakeArtifactServerForLocalTests(t *testing.T) {
	cfg := config.Default()
	cfg.ApplianceProfile = "storage"
	cfg.ArtifactServerAllowFake = true
	cfg.ArtifactServerBaseURL = ""
	if err := cfg.Validate(); err != nil {
		t.Fatalf("explicit local fake artifact server should remain valid: %v", err)
	}
}

func TestFilesProfilesRequireAbsoluteFilesRootDir(t *testing.T) {
	cfg := config.Default()
	cfg.ApplianceProfile = "core"
	cfg.FilesRootDir = "relative/files"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "filesRootDir") {
		t.Fatalf("Validate with relative filesRootDir = %v, want filesRootDir error", err)
	}
}

func testBuildCatalog() devflows.Catalog {
	return devflows.Catalog{
		WorkProfiles: []devflows.WorkProfile{{Name: "builder", Description: "Builder workflows", Repos: []devflows.ProfileRepo{{Name: "app", EnabledByDefault: true}}}},
		Repos:        []devflows.Repo{{Name: "app", URL: "https://git.internal.example.com/team/app.git", DefaultRef: "0123456789abcdef0123456789abcdef01234567"}},
		BuildTargets: []devflows.BuildTarget{{Name: "default", Aliases: []string{"app"}, Repo: "app", Execution: devflows.ExecutionScript, ImageRepository: "users/alice/app", ImageTagTemplate: "{commit12}"}},
	}
}

func TestLoadAppliesBuildCatalogJSON(t *testing.T) {
	jsonCatalog := `{"workProfiles":[{"name":"builder","repos":[{"name":"app","enabledByDefault":true}]}],"repos":[{"name":"app","url":"https://git.internal.example.com/team/app.git","defaultRef":"0123456789abcdef0123456789abcdef01234567"}],"buildTargets":[{"name":"default","aliases":["app"],"repo":"app","execution":"script","args":["build.sh"],"imageRepository":"users/alice/app"}]}`
	cfg, err := config.Load([]string{
		"APPLIANCE_PROFILE=builder",
		"APPLIANCE_BUILD_CATALOG_JSON=" + jsonCatalog,
		"APPLIANCE_WORKSPACE_PROVISIONER_IMAGE_DIGEST=workspace-provisioner@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"APPLIANCE_BUILDER_IMAGE_DIGEST=buildah@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.BuildCatalog.BuildTargets) != 1 {
		t.Fatalf("BuildCatalog targets = %+v, want one", cfg.BuildCatalog.BuildTargets)
	}
}

func TestLoadAppliesApplianceCatalogJSON(t *testing.T) {
	catalogJSON := `{"version":"appliance.catalog/v1alpha1","profiles":[{"name":"custom","capabilities":["base","host"]}],"modules":[{"name":"host-agent","kind":"platform","requiredCapabilities":["host"],"executionMode":"host-agent","entitlementKey":"host-agent","baseURL":"http://host-agent.ace-apps.svc.cluster.local:8080","securityClass":"host-privileged"}]}`
	cfg, err := config.Load([]string{
		"APPLIANCE_PROFILE=custom",
		"APPLIANCE_CATALOG_JSON=" + catalogJSON,
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	resolved, err := cfg.ResolveProfile()
	if err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}
	if resolved.Name != "custom" {
		t.Fatalf("resolved profile = %q, want custom", resolved.Name)
	}
	modules, err := cfg.ResolveModules(resolved)
	if err != nil {
		t.Fatalf("ResolveModules: %v", err)
	}
	if !appliance.ModuleEnabled(modules, appliance.ModuleNameHostAgent) {
		t.Fatalf("resolved modules = %+v, want host-agent enabled", modules)
	}
}

func TestLoadAppliesServiceRegistryJSON(t *testing.T) {
	registryJSON := `{"services":[{"name":"host-agent","capability":"host","baseURL":"http://127.0.0.1:18086","routes":[{"method":"GET","externalPath":"/api/v1/host/info","upstreamPath":"/internal/v1/host/info","permission":"host.read"}]}]}`
	cfg, err := config.Load([]string{
		"APPLIANCE_PROFILE=core",
		"APPLIANCE_SERVICE_REGISTRY_JSON=" + registryJSON,
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.ServiceRegistry.Services) != 1 {
		t.Fatalf("ServiceRegistry = %+v, want one service", cfg.ServiceRegistry.Services)
	}
	svc := cfg.ServiceRegistry.Services[0]
	if svc.Name != "host-agent" || svc.Capability != appliance.CapabilityHost || svc.BaseURL != "http://127.0.0.1:18086" {
		t.Fatalf("service = %+v", svc)
	}
}

func TestValidateRejectsServiceRegistryCapabilityMismatch(t *testing.T) {
	cfg := config.Default()
	cfg.ApplianceProfile = "core"
	cfg.ServiceRegistry.Services = []serviceregistry.Service{{
		Name:       "artifact-server-proxy",
		Capability: appliance.CapabilityArtifact,
		BaseURL:    "http://appliance-registry.artifacts.svc.cluster.local:5000",
		Routes: []serviceregistry.Route{
			{Method: "GET", ExternalPath: "/api/v1/artifact-server/health", UpstreamPath: "/healthz", Permission: "registry.read"},
		},
	}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "not enabled") {
		t.Fatalf("Validate with capability mismatch = %v, want not enabled error", err)
	}
}

func TestValidateRejectsMalformedServiceRegistryRoute(t *testing.T) {
	cfg := config.Default()
	cfg.ServiceRegistry.Services = []serviceregistry.Service{{
		Name:       "host-agent",
		Capability: appliance.CapabilityHost,
		BaseURL:    "http://127.0.0.1:18086",
		Routes: []serviceregistry.Route{
			{Method: "TRACE", ExternalPath: "api/v1/host/info", UpstreamPath: "/internal/v1/host/info", Permission: ""},
		},
	}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "serviceRegistry") {
		t.Fatalf("Validate with malformed registry route = %v, want serviceRegistry error", err)
	}
}

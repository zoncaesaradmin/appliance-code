package httpapi_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"appliance-code/services/controlplane/internal/app"
	"appliance-code/services/controlplane/internal/appliance"
	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/blobstore"
	"appliance-code/services/controlplane/internal/bootstrap"
	"appliance-code/services/controlplane/internal/config"
	"appliance-code/services/controlplane/internal/devflows"
	"appliance-code/services/controlplane/internal/httpapi"
	"appliance-code/services/controlplane/internal/logging"
	"appliance-code/services/controlplane/internal/mcp"
	"appliance-code/services/controlplane/internal/roles"
	"appliance-code/services/controlplane/internal/serviceregistry"
	"appliance-code/services/controlplane/internal/storage"
)

func testBuildCatalog() devflows.Catalog {
	return devflows.Catalog{
		WorkProfiles: []devflows.WorkProfile{{Name: "builder", Description: "Builder workflows", Repos: []devflows.ProfileRepo{{Name: "app", EnabledByDefault: true}}}},
		Repos:        []devflows.Repo{{Name: "app", URL: "https://git.internal.example.com/team/app.git", DefaultRef: "0123456789abcdef0123456789abcdef01234567"}},
		BuildTargets: []devflows.BuildTarget{{Name: "default", Aliases: []string{"app"}, Repo: "app", Execution: devflows.ExecutionScript, ImageRepository: "users/alice/app", ImageTagTemplate: "{commit12}"}},
	}
}

type testServer struct {
	*httptest.Server
	services           *app.Services
	blobStore          *fakeS3
	videoStore         *fakeS3 // alias of blobStore for video tests
	videoProjectionDir string
}

func newTestServer(t *testing.T) *testServer {
	return newTestServerWithCatalog(t, appliance.ProfileBuilderStorageLANDNS, testBuildCatalog())
}

func newTestServerWithProfile(t *testing.T, profile appliance.Profile) *testServer {
	return newTestServerWithCatalog(t, profile, testBuildCatalog())
}

func newTestServerWithCatalog(t *testing.T, profile appliance.Profile, catalog devflows.Catalog) *testServer {
	t.Helper()
	cfg, err := config.Load(nil)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	cfg.DataDir = t.TempDir()
	cfg.ApplianceProfile = string(profile)
	hostUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/internal/v1/host/info":
			_ = json.NewEncoder(w).Encode(map[string]any{"hostname": "appliance-01", "operatingSystem": "Ubuntu 24.04.2 LTS"})
		case "/internal/v1/host/stats":
			_ = json.NewEncoder(w).Encode(map[string]any{"uptimeSeconds": 123.45, "logicalCpuCount": 8})
		case "/internal/v1/host/health":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "hostRootAccessible": true})
		case "/internal/v1/host/wifi":
			if r.Method == http.MethodGet || r.Method == http.MethodPut {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"desired": false, "actual": "inactive", "reason": "desired_off",
					"iface": "wlp2s0", "security": "open", "supportedCapable": true,
					"supportsConcurrentAP": false,
					"concurrentAPDetail":   "Client Wi-Fi and Wi-Fi AP need separate wireless interfaces on this appliance.",
				})
				return
			}
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		case "/internal/v1/host/wifi/enable":
			if r.Method == http.MethodPut {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"desired": false, "actual": "inactive", "reason": "desired_off",
					"radioEnabled": true, "iface": "wlp2s0", "security": "unknown", "supportedCapable": true,
				})
				return
			}
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		case "/internal/v1/host/wifi/scan":
			if r.Method == http.MethodGet {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"iface": "wlp2s0", "supportedCapable": true,
					"networks": []map[string]any{
						{"ssid": "office", "security": "wpa2-psk", "requiresPassword": true, "signalDBM": -40},
					},
				})
				return
			}
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		case "/internal/v1/host/wifi-ap":
			if r.Method == http.MethodGet || r.Method == http.MethodPut {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"desired": false, "actual": "inactive", "reason": "desired_off",
					"managementAddress": "10.42.0.1", "managementHostname": "manage.ap",
					"managementURL": "https://manage.ap/", "security": "wpa2-psk",
				})
				return
			}
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		case "/internal/v1/host/mdns":
			if r.Method == http.MethodGet || r.Method == http.MethodPut {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"desired": false, "actual": "inactive", "reason": "desired_off",
					"service": "avahi-daemon.service", "supportedCapable": true,
					"advertisedName": "appliance-01.local",
				})
				return
			}
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(hostUpstream.Close)
	resolved, err := appliance.ResolveProfile(string(profile))
	if err != nil {
		t.Fatalf("ResolveProfile(%s): %v", profile, err)
	}
	modules, err := appliance.EmbeddedModuleCatalog()
	if err != nil {
		t.Fatalf("EmbeddedModuleCatalog: %v", err)
	}
	modules = appliance.ResolveModules(resolved, appliance.AlwaysEntitled{}, modules)
	for i := range modules {
		if modules[i].Name == "host-agent" {
			modules[i].BaseURL = hostUpstream.URL
		}
	}
	cfg.ServiceRegistry = serviceregistry.RegistryFromModules(modules)
	if resolved.Capabilities.Enabled(appliance.CapabilityBuild) {
		cfg.BuildCatalog = catalog
		cfg.WorkspaceProvisionerImageDigest = "workspace-provisioner@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		cfg.BuilderImageDigest = "buildah@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	}
	var blobStore *fakeS3
	if resolved.Capabilities.Enabled(appliance.CapabilityFiles) || resolved.Capabilities.Enabled(appliance.CapabilityVideo) {
		blobStore = newFakeS3(t)
		cfg.BlobStorageEndpoint = blobStore.URL
		cfg.BlobStorageBucket = "appliance"
		cfg.BlobStorageAccessKey = "test-access-key"
		cfg.BlobStorageSecretKey = "test-secret-key"
		cfg.VideoLibraryProjectionDir = t.TempDir()
	}
	if resolved.Capabilities.Enabled(appliance.CapabilityDNS) {
		cfg.DNSReadyURL = "http://dns-server.dns.svc.cluster.local:8181/ready"
		cfg.DNSAllowFakeZoneSync = true
	}

	logger, err := logging.New("error")
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}
	services, err := app.WireServices(cfg, logger)
	if err != nil {
		t.Fatalf("WireServices: %v", err)
	}
	t.Cleanup(func() { services.DB.Close() })
	if resolved.Capabilities.Enabled(appliance.CapabilityBuild) && services.BuilderGit != nil {
		hosts, err := catalog.RepoHosts()
		if err != nil {
			t.Fatalf("catalog.RepoHosts: %v", err)
		}
		if len(hosts) > 0 {
			if _, err := services.BuilderGit.Upsert(t.Context(), "git-internal", hosts[0], "builder-user", "builder-token"); err != nil {
				t.Fatalf("BuilderGit.Upsert: %v", err)
			}
		}
	}

	authDeps := httpapi.AuthDeps{
		Sessions: services.Sessions, Tokens: services.Tokens, Authz: services.Authz,
		Users: services.Users, Roles: services.Roles,
	}
	deps := httpapi.Deps{
		Logger:        logger,
		Auth:          authDeps,
		AuthH:         &httpapi.AuthHandlers{Sessions: services.Sessions, Users: services.Users, UserStore: services.UserStore},
		SetupH:        &httpapi.SetupHandlers{DB: services.DB, UserStore: services.UserStore, RoleStore: services.RoleStore, Users: services.Users},
		CapabilitiesH: &httpapi.CapabilitiesHandlers{Capabilities: services.ApplianceProfile.Capabilities},
		IdentityH: &httpapi.IdentityHandlers{
			ApplianceName:   cfg.ApplianceName,
			DNSZone:         cfg.DNSZoneName,
			NodeIPv4:        cfg.NodeIPv4,
			CanonicalOrigin: cfg.CanonicalOrigin,
		},
		ForwardAuthH: &httpapi.ForwardAuthHandlers{
			Auth: authDeps, Audit: services.Audit, Capabilities: services.ApplianceProfile.Capabilities,
		},
		UsersH:         &httpapi.UserHandlers{Users: services.Users, Roles: services.Roles},
		RolesH:         &httpapi.RoleHandlers{Roles: services.Roles},
		TokensH:        &httpapi.TokenHandlers{Tokens: services.Tokens},
		LANDNSPublishH: &httpapi.LANDNSPublishHandlers{Audit: services.Audit},
		LicensingH:     &httpapi.LicensingHandlers{Licensing: services.Licensing},
		SetupStateH: &httpapi.SetupStateHandlers{
			Licensing: services.Licensing, Profiles: services.Profiles, Metadata: services.Metadata,
			Notifications: services.Notifications, RuntimeProfile: string(services.ApplianceProfile.Name),
		},
		NotificationsH: &httpapi.NotificationHandlers{Notifications: services.Notifications, Audit: services.Audit},
		ProfilesH:      &httpapi.ProfileHandlers{Profiles: services.Profiles},
		MetadataH:      &httpapi.MetadataBundleHandlers{Metadata: services.Metadata},
		ApplicationsH:  &httpapi.ApplicationHandlers{Applications: services.Applications},
		AuditH: &httpapi.AuditHandlers{
			Store: services.AuditStore, Ops: services.AuditOps, CursorKey: services.Keys.CursorHMACKey,
		},
		ProxiedServices: httpapi.RegistrationsFromRegistry(cfg.ServiceRegistry),
		Audit:           services.Audit,
		MCPHandler: mcp.NewHandler(authDeps, cfg.CanonicalOrigin,
			mcp.WithDeveloperWorkflows(services.Devflows, services.ApplianceProfile.Capabilities)),
	}
	if appliance.ModuleEnabled(services.Modules, appliance.ModuleNameArtifactRegistry) {
		deps.RegistryH = &httpapi.RegistryTokenHandlers{
			Auth: authDeps, Users: services.Users, Authorizer: services.RegistryAuthorizer,
			Keys: services.Keys, Issuer: cfg.CanonicalOrigin,
		}
		deps.RegistryGrantsH = &httpapi.RegistryGrantHandlers{Grants: services.RegistryGrantStore, Audit: services.Audit}
		deps.RegistryCatalogH = &httpapi.RegistryCatalogHandlers{
			ArtifactServer: services.ArtifactServer, Authorizer: services.RegistryAuthorizer, Users: services.Users,
		}
	}
	if appliance.ModuleEnabled(services.Modules, appliance.ModuleNameFiles) || resolved.Capabilities.Enabled(appliance.CapabilityVideo) {
		blobClient, err := blobstore.New(cfg.BlobStorageEndpoint, cfg.BlobStorageBucket, cfg.BlobStorageAccessKey, cfg.BlobStorageSecretKey, cfg.BlobStorageRegion)
		if err != nil {
			t.Fatalf("blobstore.New: %v", err)
		}
		if appliance.ModuleEnabled(services.Modules, appliance.ModuleNameFiles) {
			deps.FilesH = &httpapi.FileHandlers{
				Store:           blobClient,
				ObjectPrefix:    cfg.FilesObjectPrefix,
				MaxUploadBytes:  cfg.FilesMaxUploadBytes,
				TransferTimeout: cfg.FilesTransferTimeout,
				Audit:           services.Audit,
			}
		}
		if resolved.Capabilities.Enabled(appliance.CapabilityVideo) {
			deps.VideoLibraryH = &httpapi.VideoLibraryHandlers{
				Store:           blobClient,
				ObjectPrefix:    cfg.VideoLibraryObjectPrefix,
				ProjectionDir:   cfg.VideoLibraryProjectionDir,
				MaxUploadBytes:  cfg.VideoMaxUploadBytes,
				TransferTimeout: cfg.VideoTransferTimeout,
				Audit:           services.Audit,
			}
		}
	}
	if resolved.Capabilities.Enabled(appliance.CapabilityFocusContent) {
		deps.FocusContentH = &httpapi.FocusContentHandlers{Store: services.FocusContentStore, Audit: services.Audit}
	}
	if appliance.ModuleEnabled(services.Modules, appliance.ModuleNameBuild) {
		deps.BuildsH = &httpapi.BuildHandlers{Builds: services.Builds}
		deps.DevflowsH = &httpapi.DeveloperWorkflowHandlers{Devflows: services.Devflows, BuilderGit: services.BuilderGit, Logger: logger, Audit: services.Audit}
	}
	if appliance.ModuleEnabled(services.Modules, appliance.ModuleNameLANDNS) {
		deps.DNSH = &httpapi.DNSHandlers{DNS: services.DNS}
	}

	handler, err := httpapi.NewPublicMux(deps, services.ApplianceProfile.Capabilities, services.Modules)
	if err != nil {
		t.Fatalf("NewPublicMux: %v", err)
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &testServer{Server: srv, services: services, blobStore: blobStore, videoStore: blobStore, videoProjectionDir: cfg.VideoLibraryProjectionDir}
}

// bootstrapAdmin creates the first administrator directly through the
// bootstrap package, mirroring how the real CLI does it.
func (ts *testServer) bootstrapAdmin(t *testing.T, username, password string) string {
	t.Helper()
	result, err := bootstrap.Init(t.Context(), ts.services.DB, ts.services.UserStore, ts.services.RoleStore, ts.services.Users, username, password, username)
	if err != nil {
		t.Fatalf("bootstrap.Init: %v", err)
	}
	return result.AdminUserID
}

// createUserWithRole creates a user assigned exactly roleID, for driving
// the RBAC probe matrix.
func (ts *testServer) createUserWithRole(t *testing.T, username, password, roleID string) string {
	t.Helper()
	actor := audit.Actor{Type: storage.AuditActorSystem, AuthMethod: "test"}
	user, err := ts.services.Users.Create(t.Context(), actor, username, username, password)
	if err != nil {
		t.Fatalf("creating user %s: %v", username, err)
	}
	if err := ts.services.Roles.SetUserRoles(t.Context(), actor, user.ID, []string{roleID}); err != nil {
		t.Fatalf("assigning role to %s: %v", username, err)
	}
	return user.ID
}

func (ts *testServer) login(t *testing.T, username, password string) string {
	t.Helper()
	resp := ts.doJSON(t, "POST", "/api/v1/auth/login", "", fmt.Sprintf(`{"username":%q,"password":%q}`, username, password))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login for %s: status = %d", username, resp.StatusCode)
	}
	var body struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding login response: %v", err)
	}
	return body.AccessToken
}

func (ts *testServer) doJSON(t *testing.T, method, path, bearer, body string) *http.Response {
	t.Helper()
	return ts.doJSONWithHeaders(t, method, path, bearer, body, nil)
}

func (ts *testServer) doJSONWithHeaders(t *testing.T, method, path, bearer, body string, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, ts.URL+path, bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

const testPassword = "a-sufficiently-long-test-password-1"

func TestCapabilitiesReflectsResolvedProfile(t *testing.T) {
	cases := []struct {
		profile appliance.Profile
		want    []string
	}{
		{appliance.ProfileCore, []string{"base", "files"}},
		{appliance.ProfileBuilderStorageLANDNS, []string{"applications", "artifact", "base", "build", "dns", "files", "host", "workflows"}},
		{appliance.ProfileBuilderLANLLMStorageLANDNS, []string{"applications", "artifact", "base", "build", "dns", "files", "host", "inference", "workflows"}},
		{appliance.ProfileTraining, []string{"applications", "base", "files", "focus-content", "guest-access", "host", "plaintext-http", "video"}},
	}
	for _, tc := range cases {
		t.Run(string(tc.profile), func(t *testing.T) {
			ts := newTestServerWithProfile(t, tc.profile)
			resp := ts.doJSON(t, "GET", "/api/v1/capabilities", "", "")
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("capabilities status = %d, want 200", resp.StatusCode)
			}
			var body struct {
				Capabilities []string `json:"capabilities"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode capabilities: %v", err)
			}
			if fmt.Sprint(body.Capabilities) != fmt.Sprint(tc.want) {
				t.Fatalf("capabilities for profile %q = %v, want %v", tc.profile, body.Capabilities, tc.want)
			}
		})
	}
}

func TestGuestAccessIsCapabilityGated(t *testing.T) {
	t.Run("enabled", func(t *testing.T) {
		ts := newTestServerWithProfile(t, appliance.ProfileTraining)
		ts.bootstrapAdmin(t, "admin", testPassword)
		resp := ts.doJSON(t, "POST", "/api/v1/auth/guest", "", `{"name":"Guest Viewer"}`)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("guest login status = %d, want 200", resp.StatusCode)
		}
		var body struct {
			AccessToken string `json:"accessToken"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decoding guest login response: %v", err)
		}
		if body.AccessToken == "" {
			t.Fatal("guest login did not return an access token")
		}

		session := ts.doJSON(t, "GET", "/api/v1/auth/session", body.AccessToken, "")
		defer session.Body.Close()
		if session.StatusCode != http.StatusOK {
			t.Fatalf("guest session status = %d, want 200", session.StatusCode)
		}
		var principal struct {
			Username    string   `json:"username"`
			DisplayName string   `json:"displayName"`
			Permissions []string `json:"permissions"`
		}
		if err := json.NewDecoder(session.Body).Decode(&principal); err != nil {
			t.Fatalf("decoding guest session: %v", err)
		}
		if principal.Username != "guest" {
			t.Fatalf("guest session username = %q, want guest", principal.Username)
		}
		if principal.DisplayName != "Guest Viewer" {
			t.Fatalf("guest session display name = %q, want Guest Viewer", principal.DisplayName)
		}
		permissions := make(map[string]bool, len(principal.Permissions))
		for _, permission := range principal.Permissions {
			permissions[permission] = true
		}
		if len(permissions) != 2 || !permissions[roles.PermVideoLibraryRead] || !permissions[roles.PermVideoPlay] {
			t.Fatalf("guest permissions = %v, want video read/play only", principal.Permissions)
		}
	})

	t.Run("disabled", func(t *testing.T) {
		ts := newTestServerWithProfile(t, appliance.ProfileCore)
		resp := ts.doJSON(t, "POST", "/api/v1/auth/guest", "", `{"name":"Guest Viewer"}`)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("guest login without capability status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("requires name", func(t *testing.T) {
		ts := newTestServerWithProfile(t, appliance.ProfileTraining)
		ts.bootstrapAdmin(t, "admin", testPassword)
		resp := ts.doJSON(t, "POST", "/api/v1/auth/guest", "", `{}`)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("guest login without name status = %d, want 400", resp.StatusCode)
		}
	})
}

func TestCoreProfileDoesNotExposeArtifactRoutes(t *testing.T) {
	ts := newTestServerWithProfile(t, appliance.ProfileCore)
	resp := ts.doJSON(t, "GET", "/api/v1/registry/repositories", "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("core artifact route status = %d, want 404", resp.StatusCode)
	}
}

func TestHostRoutesProxyThroughControlPlane(t *testing.T) {
	ts := newTestServerWithProfile(t, appliance.ProfileTraining)
	ts.bootstrapAdmin(t, "admin", testPassword)
	token := ts.login(t, "admin", testPassword)

	for _, path := range []string{"/api/v1/host/info", "/api/v1/host/stats", "/api/v1/host/health", "/api/v1/host/wifi", "/api/v1/host/wifi/scan", "/api/v1/host/wifi-ap", "/api/v1/host/mdns"} {
		resp := ts.doJSON(t, http.MethodGet, path, token, "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", path, resp.StatusCode)
		}
		resp.Body.Close()
	}
	resp := ts.doJSON(t, http.MethodPut, "/api/v1/host/wifi/enable", token, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("wifi enable status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	resp = ts.doJSON(t, http.MethodGet, "/api/v1/host/mdns", token, "")
	defer resp.Body.Close()
	var mdns struct {
		AdvertisedName string `json:"advertisedName"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&mdns); err != nil {
		t.Fatalf("decode host mdns: %v", err)
	}
	if mdns.AdvertisedName != "appliance-01.local" {
		t.Fatalf("advertisedName = %q, want appliance-01.local", mdns.AdvertisedName)
	}
}

func TestApplicationsUseOnlyReleaseCatalogAndAdminLifecycle(t *testing.T) {
	ts := newTestServerWithProfile(t, appliance.ProfileTraining)
	ts.bootstrapAdmin(t, "admin", testPassword)
	token := ts.login(t, "admin", testPassword)

	resp := ts.doJSON(t, http.MethodGet, "/api/v1/applications", token, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list applications status = %d", resp.StatusCode)
	}
	var catalog struct {
		Items []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&catalog); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(catalog.Items) != 1 || catalog.Items[0].Name != "jellyfin" || catalog.Items[0].Version != "10.10.7" {
		t.Fatalf("catalog = %+v", catalog.Items)
	}

	resp = ts.doJSON(t, http.MethodPost, "/api/v1/applications/jellyfin/install", token, `{"version":"10.10.7"}`)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("install status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = ts.doJSON(t, http.MethodPost, "/api/v1/applications/jellyfin/disable", token, "")
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("disable status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = ts.doJSON(t, http.MethodPost, "/api/v1/applications", token, `{}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("mutable application registration status = %d, want 404", resp.StatusCode)
	}
}

func TestSetupStatusAndFirstAdminFlow(t *testing.T) {
	ts := newTestServer(t)

	resp := ts.doJSON(t, "GET", "/api/v1/setup/status", "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("initial setup status = %d, want 200", resp.StatusCode)
	}
	var status struct {
		Initialized bool `json:"initialized"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("decode setup status: %v", err)
	}
	if status.Initialized {
		t.Fatal("expected fresh test appliance to report initialized=false")
	}

	resp = ts.doJSON(t, "POST", "/api/v1/setup/first-admin", "", `{"username":"admin","password":"`+testPassword+`"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create first admin status = %d, want 201", resp.StatusCode)
	}
	resp.Body.Close()

	resp = ts.doJSON(t, "GET", "/api/v1/setup/status", "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post-bootstrap setup status = %d, want 200", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("decode setup status after bootstrap: %v", err)
	}
	if !status.Initialized {
		t.Fatal("expected initialized=true after creating first admin")
	}

	token := ts.login(t, "admin", testPassword)
	resp = ts.doJSON(t, "GET", "/api/v1/auth/session", token, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("session after setup status = %d, want 200", resp.StatusCode)
	}
}

func TestSetupCreateFirstAdminRejectsAlreadyInitializedAppliance(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)

	resp := ts.doJSON(t, "POST", "/api/v1/setup/first-admin", "", `{"username":"second","password":"`+testPassword+`"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("create first admin on initialized appliance = %d, want 409", resp.StatusCode)
	}
}

func TestLoginHTTPEndToEnd(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)

	resp := ts.doJSON(t, "POST", "/api/v1/auth/login", "", `{"username":"admin","password":"wrong"}`)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong password status = %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()

	token := ts.login(t, "admin", testPassword)
	resp = ts.doJSON(t, "GET", "/api/v1/auth/session", token, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("session status = %d, want 200", resp.StatusCode)
	}
	var session struct {
		Username   string `json:"username"`
		Domain     string `json:"domain"`
		AuthMethod string `json:"authMethod"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if session.Username != "admin" {
		t.Errorf("session.username = %q, want admin", session.Username)
	}
	if session.Domain != "local" {
		t.Errorf("session.domain = %q, want local", session.Domain)
	}
	if session.AuthMethod != "session" {
		t.Errorf("session.authMethod = %q, want session", session.AuthMethod)
	}
}

func TestLoginRejectsUnsupportedAuthDomain(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)

	resp := ts.doJSON(t, "POST", "/api/v1/auth/login", "", `{"username":"admin","password":"`+testPassword+`","domain":"ldap"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unsupported domain status = %d, want 400", resp.StatusCode)
	}
}

func TestLoginDefaultsOmittedOrEmptyDomainToLocal(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)

	cases := []struct {
		name string
		body string
	}{
		{name: "omitted", body: fmt.Sprintf(`{"username":"admin","password":%q}`, testPassword)},
		{name: "empty", body: fmt.Sprintf(`{"username":"admin","password":%q,"domain":""}`, testPassword)},
		{name: "whitespace", body: fmt.Sprintf(`{"username":"admin","password":%q,"domain":"  "}`, testPassword)},
		{name: "explicit local", body: fmt.Sprintf(`{"username":"admin","password":%q,"domain":"local"}`, testPassword)},
		{name: "explicit LOCAL", body: fmt.Sprintf(`{"username":"admin","password":%q,"domain":"LOCAL"}`, testPassword)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := ts.doJSON(t, "POST", "/api/v1/auth/login", "", tc.body)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("login status = %d, want 200", resp.StatusCode)
			}
			var login struct {
				AccessToken string `json:"accessToken"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&login); err != nil {
				t.Fatalf("decode login: %v", err)
			}
			sessionResp := ts.doJSON(t, "GET", "/api/v1/auth/session", login.AccessToken, "")
			defer sessionResp.Body.Close()
			if sessionResp.StatusCode != http.StatusOK {
				t.Fatalf("session status = %d, want 200", sessionResp.StatusCode)
			}
			var session struct {
				Domain string `json:"domain"`
			}
			if err := json.NewDecoder(sessionResp.Body).Decode(&session); err != nil {
				t.Fatalf("decode session: %v", err)
			}
			if session.Domain != "local" {
				t.Fatalf("session.domain = %q, want local", session.Domain)
			}
		})
	}
}

func TestChangePasswordRequiresCurrentPasswordAndForcesReLogin(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)

	token := ts.login(t, "admin", testPassword)
	const nextPassword = "a-brand-new-long-password-99"

	resp := ts.doJSON(t, "POST", "/api/v1/auth/password", token, fmt.Sprintf(
		`{"currentPassword":%q,"newPassword":%q}`, "wrong-current-password", nextPassword,
	))
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong current password status = %d, want 401", resp.StatusCode)
	}

	resp = ts.doJSON(t, "POST", "/api/v1/auth/password", token, fmt.Sprintf(
		`{"currentPassword":%q,"newPassword":%q}`, testPassword, nextPassword,
	))
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("change password status = %d, want 204", resp.StatusCode)
	}

	resp = ts.doJSON(t, "GET", "/api/v1/auth/session", token, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old session after password change = %d, want 401", resp.StatusCode)
	}

	resp = ts.doJSON(t, "POST", "/api/v1/auth/login", "", fmt.Sprintf(
		`{"username":"admin","password":%q}`, testPassword,
	))
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old password login = %d, want 401", resp.StatusCode)
	}

	newToken := ts.login(t, "admin", nextPassword)
	resp = ts.doJSON(t, "GET", "/api/v1/auth/session", newToken, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("session with new password = %d, want 200", resp.StatusCode)
	}
}

func TestChangePasswordRejectsUnauthenticatedCallers(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)

	resp := ts.doJSON(t, "POST", "/api/v1/auth/password", "", `{"currentPassword":"x","newPassword":"y-long-enough"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated change password = %d, want 401", resp.StatusCode)
	}
}

func TestUnauthenticatedRequestsAreRejected(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)

	for _, path := range []string{"/api/v1/users", "/api/v1/roles", "/api/v1/tokens", "/api/v1/permissions"} {
		resp := ts.doJSON(t, "GET", path, "", "")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("GET %s without credentials = %d, want 401", path, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

func TestForwardAuthCheckAllowsAuthorizedMCPRequest(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)

	token := ts.login(t, "admin", testPassword)
	resp := ts.doJSONWithHeaders(t, "GET", "/internal/auth/check", token, "", map[string]string{
		"X-Forwarded-Method": "POST",
		"X-Forwarded-Uri":    "/mcp",
		"X-Forwarded-Host":   "appliance.example.internal",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("forward auth status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Appliance-User-Id"); got == "" {
		t.Error("forward auth should return X-Appliance-User-Id")
	}
	if got := resp.Header.Get("X-Appliance-Username"); got != "admin" {
		t.Errorf("X-Appliance-Username = %q, want admin", got)
	}
	if got := resp.Header.Get("X-Appliance-Scopes"); !strings.Contains(got, roles.PermMCPInvoke) {
		t.Errorf("X-Appliance-Scopes = %q, want to contain %q", got, roles.PermMCPInvoke)
	}
	if got := resp.Header.Get("X-Appliance-Roles"); !strings.Contains(got, "administrator") {
		t.Errorf("X-Appliance-Roles = %q, want administrator", got)
	}
	if got := resp.Header.Get("X-Appliance-Auth-Domain"); got != "local" {
		t.Errorf("X-Appliance-Auth-Domain = %q, want local", got)
	}
	if got := resp.Header.Get("X-Appliance-Auth-Method"); got != "session" {
		t.Errorf("X-Appliance-Auth-Method = %q, want session", got)
	}
}

func TestForwardAuthCheckRejectsUnauthenticatedRequest(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)

	resp := ts.doJSONWithHeaders(t, "GET", "/internal/auth/check", "", "", map[string]string{
		"X-Forwarded-Method": "POST",
		"X-Forwarded-Uri":    "/mcp",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("forward auth unauthenticated status = %d, want 401", resp.StatusCode)
	}
}

func TestForwardAuthCheckRejectsUnauthorizedRequest(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)
	ts.createUserWithRole(t, "viewer-user", testPassword, roles.ViewerRoleID)

	token := ts.login(t, "viewer-user", testPassword)
	resp := ts.doJSONWithHeaders(t, "GET", "/internal/auth/check", token, "", map[string]string{
		"X-Forwarded-Method": "POST",
		"X-Forwarded-Uri":    "/mcp",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("forward auth unauthorized status = %d, want 403", resp.StatusCode)
	}
}

func TestDisabledCapabilityRoutesReturnNotFound(t *testing.T) {
	ts := newTestServerWithProfile(t, appliance.ProfileCore)
	ts.bootstrapAdmin(t, "admin", testPassword)
	token := ts.login(t, "admin", testPassword)

	for _, path := range []string{
		"/api/v1/builds",
		"/api/v1/registry/grants",
		"/api/v1/registry/repositories",
	} {
		resp := ts.doJSON(t, "GET", path, token, "")
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want 404", path, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

func TestForwardAuthCheckReturnsNotFoundWhenArtifactCapabilityDisabled(t *testing.T) {
	ts := newTestServerWithProfile(t, appliance.ProfileCore)
	ts.bootstrapAdmin(t, "admin", testPassword)
	token := ts.login(t, "admin", testPassword)

	resp := ts.doJSONWithHeaders(t, "GET", "/internal/auth/check", token, "", map[string]string{
		"X-Forwarded-Method": "GET",
		"X-Forwarded-Uri":    "/v2/library/nginx/manifests/latest",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("forward auth status = %d, want 404", resp.StatusCode)
	}
}

// TestAuthorizationMatrix drives every built-in role against a
// representative probe route for each permission family and asserts the
// route's allow/deny behavior always matches the published permission
// catalog in internal/roles, which is the plan's required table-driven
// authorization proof.
func TestAuthorizationMatrix(t *testing.T) {
	type probe struct {
		name       string
		method     string
		path       string
		body       string
		permission string
	}
	probes := []probe{
		{"users.read", "GET", "/api/v1/users", "", roles.PermUsersRead},
		{"roles.read", "GET", "/api/v1/roles", "", roles.PermRolesRead},
		{"permissions.read", "GET", "/api/v1/permissions", "", roles.PermRolesRead},
		{"tokens.read.self", "GET", "/api/v1/tokens", "", roles.PermTokensReadSelf},
		{"tokens.create.self", "POST", "/api/v1/tokens", `{"name":"probe"}`, roles.PermTokensCreateSelf},
	}

	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)

	for _, br := range roles.BuiltInRoles {
		hasPermission := make(map[string]bool, len(br.Permissions))
		for _, p := range br.Permissions {
			hasPermission[p] = true
		}

		username := "probe-" + br.Name
		ts.createUserWithRole(t, username, testPassword, br.ID)
		token := ts.login(t, username, testPassword)

		for _, p := range probes {
			t.Run(br.Name+"/"+p.name, func(t *testing.T) {
				resp := ts.doJSON(t, p.method, p.path, token, p.body)
				defer resp.Body.Close()

				denied := resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized
				wantDenied := !hasPermission[p.permission]
				if denied != wantDenied {
					t.Errorf("role %s calling %s %s (needs %s, has=%v): status=%d, denied=%v, want denied=%v",
						br.Name, p.method, p.path, p.permission, hasPermission[p.permission], resp.StatusCode, denied, wantDenied)
				}
			})
		}
	}
}

func TestRESTAndMCPDeveloperWorkflowAuthorizationEquivalence(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)
	actor := audit.Actor{Type: storage.AuditActorSystem, AuthMethod: "test"}

	createRoleUser := func(roleName, username string, permissions []string) string {
		t.Helper()
		role, err := ts.services.Roles.Create(t.Context(), actor, roleName, permissions)
		if err != nil {
			t.Fatalf("creating role %s: %v", roleName, err)
		}
		user, err := ts.services.Users.Create(t.Context(), actor, username, username, testPassword)
		if err != nil {
			t.Fatalf("creating user %s: %v", username, err)
		}
		if err := ts.services.Roles.SetUserRoles(t.Context(), actor, user.ID, []string{role.ID}); err != nil {
			t.Fatalf("assigning role to %s: %v", username, err)
		}
		return ts.login(t, username, testPassword)
	}

	mcpInitialize := func(token string) string {
		t.Helper()
		body := fmt.Sprintf(`{"jsonrpc":"2.0","id":"init","method":"initialize","params":{"protocolVersion":%q,"capabilities":{},"clientInfo":{"name":"test-client","version":"1.0"}}}`, mcp.ProtocolVersion)
		resp := ts.doJSONWithHeaders(t, http.MethodPost, "/mcp", token, body, map[string]string{"Accept": "application/json, text/event-stream"})
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("MCP initialize status = %d, want 200", resp.StatusCode)
		}
		sessionID := resp.Header.Get(mcp.SessionIDHeader)
		if sessionID == "" {
			t.Fatal("MCP initialize response missing session id")
		}
		return sessionID
	}

	mcpCallListWorkProfiles := func(token, sessionID string) (int, *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}) {
		t.Helper()
		resp := ts.doJSONWithHeaders(t, http.MethodPost, "/mcp", token, `{"jsonrpc":"2.0","id":"profiles","method":"tools/call","params":{"name":"list_work_profiles","arguments":{}}}`, map[string]string{
			"Accept":            "application/json, text/event-stream",
			mcp.SessionIDHeader: sessionID,
		})
		defer resp.Body.Close()
		var parsed struct {
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
			t.Fatalf("decoding MCP response: %v", err)
		}
		if resp.StatusCode == http.StatusOK && parsed.Error == nil && len(parsed.Result) == 0 {
			t.Fatal("MCP response had neither result nor error")
		}
		return resp.StatusCode, parsed.Error
	}

	allowedToken := createRoleUser("rest-mcp-work-profiles", "rest-mcp-user", []string{roles.PermMCPInvoke, roles.PermWorkProfilesRead})
	deniedToken := createRoleUser("mcp-without-work-profiles", "mcp-no-work-profile-user", []string{roles.PermMCPInvoke})

	for _, tc := range []struct {
		name            string
		token           string
		wantRESTStatus  int
		wantMCPJSONCode int
	}{
		{name: "allowed", token: allowedToken, wantRESTStatus: http.StatusOK, wantMCPJSONCode: 0},
		{name: "missing operation permission", token: deniedToken, wantRESTStatus: http.StatusForbidden, wantMCPJSONCode: mcp.ErrCodeInvalidRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rest := ts.doJSON(t, http.MethodGet, "/api/v1/work-profiles", tc.token, "")
			_ = rest.Body.Close()
			if rest.StatusCode != tc.wantRESTStatus {
				t.Fatalf("REST work-profiles status = %d, want %d", rest.StatusCode, tc.wantRESTStatus)
			}

			sessionID := mcpInitialize(tc.token)
			status, rpcErr := mcpCallListWorkProfiles(tc.token, sessionID)
			if status != http.StatusOK {
				t.Fatalf("MCP list_work_profiles HTTP status = %d, want 200 JSON-RPC response", status)
			}
			if tc.wantMCPJSONCode == 0 {
				if rpcErr != nil {
					t.Fatalf("MCP list_work_profiles error = %+v, want result", rpcErr)
				}
				return
			}
			if rpcErr == nil || rpcErr.Code != tc.wantMCPJSONCode {
				t.Fatalf("MCP list_work_profiles error = %+v, want JSON-RPC code %d", rpcErr, tc.wantMCPJSONCode)
			}
		})
	}
}

func TestLastAdministratorInvariantOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	adminID := ts.bootstrapAdmin(t, "admin", testPassword)
	token := ts.login(t, "admin", testPassword)

	resp := ts.doJSON(t, "POST", "/api/v1/users/"+adminID+"/disable", token, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("disabling the last administrator over HTTP = %d, want 409", resp.StatusCode)
	}
}

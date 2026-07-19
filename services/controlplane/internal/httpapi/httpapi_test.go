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
	"appliance-code/services/controlplane/internal/bootstrap"
	"appliance-code/services/controlplane/internal/config"
	"appliance-code/services/controlplane/internal/devflows"
	"appliance-code/services/controlplane/internal/httpapi"
	"appliance-code/services/controlplane/internal/logging"
	"appliance-code/services/controlplane/internal/mcp"
	"appliance-code/services/controlplane/internal/roles"
	"appliance-code/services/controlplane/internal/storage"
)

func testBuildCatalog() devflows.Catalog {
	return devflows.Catalog{
		WorkProfiles: []devflows.WorkProfile{{Name: "builder", Description: "Builder workflows", Repos: []devflows.ProfileRepo{{Name: "app", EnabledByDefault: true}}}},
		Repos:        []devflows.Repo{{Name: "app", URL: "https://git.internal.example.com/team/app.git", DefaultRef: "0123456789abcdef0123456789abcdef01234567"}},
		BuildTargets: []devflows.BuildTarget{{Name: "default", Aliases: []string{"app"}, Repo: "app", Execution: devflows.ExecutionRepoScript, ImageRepository: "users/alice/app", ImageTagTemplate: "{commit12}", BuilderImageDigest: "buildah@sha256:approved"}},
	}
}

type testServer struct {
	*httptest.Server
	services *app.Services
}

func newTestServer(t *testing.T) *testServer {
	return newTestServerWithCatalog(t, appliance.ProfileBuilder, testBuildCatalog())
}

func newTestServerWithProfile(t *testing.T, profile appliance.Profile) *testServer {
	return newTestServerWithCatalog(t, profile, testBuildCatalog())
}

func newTestServerWithCatalog(t *testing.T, profile appliance.Profile, catalog devflows.Catalog) *testServer {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.ApplianceProfile = string(profile)
	if profile == appliance.ProfileBuilder {
		cfg.BuildCatalog = catalog
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
	if profile == appliance.ProfileBuilder && services.BuilderGit != nil {
		hosts, err := catalog.RepoHosts()
		if err != nil {
			t.Fatalf("catalog.RepoHosts: %v", err)
		}
		if len(hosts) > 0 {
			if _, err := services.BuilderGit.Configure(t.Context(), hosts[0], "builder-user", "builder-token"); err != nil {
				t.Fatalf("BuilderGit.Configure: %v", err)
			}
		}
	}

	authDeps := httpapi.AuthDeps{
		Sessions: services.Sessions, Tokens: services.Tokens, Authz: services.Authz,
		Users: services.Users, Roles: services.Roles,
	}
	deps := httpapi.Deps{
		Logger: logger,
		Auth:   authDeps,
		AuthH:  &httpapi.AuthHandlers{Sessions: services.Sessions},
		SetupH: &httpapi.SetupHandlers{DB: services.DB, UserStore: services.UserStore, RoleStore: services.RoleStore, Users: services.Users},
		ForwardAuthH: &httpapi.ForwardAuthHandlers{
			Auth: authDeps, Audit: services.Audit, Capabilities: services.ApplianceProfile.Capabilities,
		},
		UsersH:  &httpapi.UserHandlers{Users: services.Users, Roles: services.Roles},
		RolesH:  &httpapi.RoleHandlers{Roles: services.Roles},
		TokensH: &httpapi.TokenHandlers{Tokens: services.Tokens},
		MCPHandler: mcp.NewHandler(authDeps, cfg.CanonicalOrigin,
			mcp.WithDeveloperWorkflows(services.Devflows, services.ApplianceProfile.Capabilities)),
	}
	if services.ApplianceProfile.Capabilities.Enabled(appliance.CapabilityArtifact) {
		deps.RegistryH = &httpapi.RegistryTokenHandlers{
			Auth: authDeps, Users: services.Users, Authorizer: services.RegistryAuthorizer,
			Keys: services.Keys, Issuer: cfg.CanonicalOrigin,
		}
		deps.RegistryGrantsH = &httpapi.RegistryGrantHandlers{Grants: services.RegistryGrantStore}
		deps.RegistryCatalogH = &httpapi.RegistryCatalogHandlers{
			Zot: services.Zot, Authorizer: services.RegistryAuthorizer, Users: services.Users,
		}
	}
	if services.ApplianceProfile.Capabilities.Enabled(appliance.CapabilityBuild) {
		deps.BuildsH = &httpapi.BuildHandlers{Builds: services.Builds}
		deps.DevflowsH = &httpapi.DeveloperWorkflowHandlers{Devflows: services.Devflows, BuilderGit: services.BuilderGit, Logger: logger}
	}

	handler, err := httpapi.NewPublicMux(deps, services.ApplianceProfile.Capabilities)
	if err != nil {
		t.Fatalf("NewPublicMux: %v", err)
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &testServer{Server: srv, services: services}
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

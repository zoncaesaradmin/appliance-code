package mcp_test

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
	"appliance-code/services/controlplane/internal/mcp"
	"appliance-code/services/controlplane/internal/reqauth"
	"appliance-code/services/controlplane/internal/roles"
	"appliance-code/services/controlplane/internal/storage"
	"appliance-code/services/controlplane/internal/workflows"
)

const canonicalOrigin = "https://appliance.example.internal"
const testPassword = "a-sufficiently-long-test-password-1"

func testBuildCatalog() devflows.Catalog {
	return devflows.Catalog{
		WorkProfiles: []devflows.WorkProfile{{Name: "builder", Description: "Builder workflows", Repos: []devflows.ProfileRepo{{Name: "app", EnabledByDefault: true}}}},
		Repos:        []devflows.Repo{{Name: "app", URL: "git@git.internal.example.com:team/app.git", DefaultRef: "0123456789abcdef0123456789abcdef01234567"}},
		BuildTargets: []devflows.BuildTarget{{Name: "default", Aliases: []string{"app"}, Repo: "app", Execution: devflows.ExecutionRepoScript, ImageRepository: "users/alice/app", ImageTagTemplate: "{commit12}", BuilderImageDigest: "buildah@sha256:approved"}},
	}
}

type testEnv struct {
	*httptest.Server
	services *app.Services
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	return newTestEnvWithProfile(t, appliance.ProfileCore)
}

func newTestEnvWithProfile(t *testing.T, profile appliance.Profile) *testEnv {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.CanonicalOrigin = canonicalOrigin
	cfg.ApplianceProfile = string(profile)
	if profile == appliance.ProfileBuilder {
		cfg.BuildCatalog = testBuildCatalog()
	}

	services, err := app.WireServices(cfg)
	if err != nil {
		t.Fatalf("WireServices: %v", err)
	}
	t.Cleanup(func() { services.DB.Close() })

	deps := reqauth.Deps{
		Sessions: services.Sessions, Tokens: services.Tokens, Authz: services.Authz,
		Users: services.Users, Roles: services.Roles,
	}
	handler := mcp.NewHandler(deps, cfg.CanonicalOrigin,
		mcp.WithDeveloperWorkflows(services.Devflows, services.ApplianceProfile.Capabilities))

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &testEnv{Server: srv, services: services}
}

func (e *testEnv) createUserWithRole(t *testing.T, username, roleID string) {
	t.Helper()
	actor := audit.Actor{Type: storage.AuditActorSystem, AuthMethod: "test"}
	user, err := e.services.Users.Create(t.Context(), actor, username, username, testPassword)
	if err != nil {
		t.Fatalf("creating user %s: %v", username, err)
	}
	if err := e.services.Roles.SetUserRoles(t.Context(), actor, user.ID, []string{roleID}); err != nil {
		t.Fatalf("assigning role to %s: %v", username, err)
	}
}

func (e *testEnv) login(t *testing.T, username string) string {
	t.Helper()
	result, err := e.services.Sessions.Login(t.Context(), "127.0.0.1", "test", username, testPassword)
	if err != nil {
		t.Fatalf("login for %s: %v", username, err)
	}
	return result.AccessToken
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (e *testEnv) post(t *testing.T, bearer, sessionID, body string) (*http.Response, rpcResponse) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, e.URL, bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if sessionID != "" {
		req.Header.Set(mcp.SessionIDHeader, sessionID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	var parsed rpcResponse
	if resp.Header.Get("Content-Type") == "application/json" {
		if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
			t.Fatalf("decoding JSON-RPC response: %v", err)
		}
	}
	return resp, parsed
}

func initializeRequest(id, protocolVersion string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"method":"initialize","params":{"protocolVersion":%q,"capabilities":{},"clientInfo":{"name":"test-client","version":"1.0"}}}`, id, protocolVersion)
}

func (e *testEnv) initializeSession(t *testing.T, bearer string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, e.URL, bytes.NewBufferString(initializeRequest("1", mcp.ProtocolVersion)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("initialize status = %d, want 200", resp.StatusCode)
	}
	sessionID := resp.Header.Get(mcp.SessionIDHeader)
	if sessionID == "" {
		t.Fatal("initialize response should carry Mcp-Session-Id")
	}
	return sessionID
}

func TestInitializeAndToolsListHappyPath(t *testing.T) {
	env := newTestEnv(t)
	if _, err := bootstrap.Init(t.Context(), env.services.DB, env.services.UserStore, env.services.RoleStore, env.services.Users, "admin", testPassword, "Administrator"); err != nil {
		t.Fatalf("bootstrap.Init: %v", err)
	}
	token := env.login(t, "admin")

	sessionID := env.initializeSession(t, token)

	resp, parsed := env.post(t, token, sessionID, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("notifications/initialized status = %d, want 202", resp.StatusCode)
	}

	resp, parsed = env.post(t, token, sessionID, `{"jsonrpc":"2.0","id":"2","method":"tools/list"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tools/list status = %d, want 200", resp.StatusCode)
	}
	if parsed.Error != nil {
		t.Fatalf("tools/list returned an error: %+v", parsed.Error)
	}
	var result struct {
		Tools []mcp.Tool `json:"tools"`
	}
	if err := json.Unmarshal(parsed.Result, &result); err != nil {
		t.Fatalf("decoding tools/list result: %v", err)
	}
	if result.Tools == nil || len(result.Tools) != 0 {
		t.Errorf("tools/list should return an empty (non-nil) list, got %v", result.Tools)
	}
}

func TestUnsupportedProtocolVersionStillNegotiates(t *testing.T) {
	env := newTestEnv(t)
	if _, err := bootstrap.Init(t.Context(), env.services.DB, env.services.UserStore, env.services.RoleStore, env.services.Users, "admin", testPassword, "Administrator"); err != nil {
		t.Fatalf("bootstrap.Init: %v", err)
	}
	token := env.login(t, "admin")

	req, _ := http.NewRequest(http.MethodPost, env.URL, bytes.NewBufferString(initializeRequest("1", "1999-01-01")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("initialize with unsupported version status = %d, want 200 (negotiation, not failure)", resp.StatusCode)
	}
	var parsed rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		t.Fatal(err)
	}
	var result mcp.InitializeResult
	if err := json.Unmarshal(parsed.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result.ProtocolVersion != mcp.ProtocolVersion {
		t.Errorf("negotiated protocol version = %q, want the server's pinned %q", result.ProtocolVersion, mcp.ProtocolVersion)
	}
}

func TestMalformedJSONReturnsParseError(t *testing.T) {
	env := newTestEnv(t)
	if _, err := bootstrap.Init(t.Context(), env.services.DB, env.services.UserStore, env.services.RoleStore, env.services.Users, "admin", testPassword, "Administrator"); err != nil {
		t.Fatalf("bootstrap.Init: %v", err)
	}
	token := env.login(t, "admin")

	resp, parsed := env.post(t, token, "", `{not valid json`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("malformed JSON status = %d, want 400", resp.StatusCode)
	}
	if parsed.Error == nil || parsed.Error.Code != mcp.ErrCodeParseError {
		t.Errorf("expected a parse-error JSON-RPC error, got %+v", parsed.Error)
	}
}

func TestUnauthenticatedRequestRejected(t *testing.T) {
	env := newTestEnv(t)
	resp, _ := env.post(t, "", "", initializeRequest("1", mcp.ProtocolVersion))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestForbiddenWithoutMCPInvokePermission(t *testing.T) {
	env := newTestEnv(t)
	if _, err := bootstrap.Init(t.Context(), env.services.DB, env.services.UserStore, env.services.RoleStore, env.services.Users, "admin", testPassword, "Administrator"); err != nil {
		t.Fatalf("bootstrap.Init: %v", err)
	}
	env.createUserWithRole(t, "viewer-user", roles.ViewerRoleID)
	token := env.login(t, "viewer-user")

	sessionID := env.initializeSession(t, token)
	resp, _ := env.post(t, token, sessionID, `{"jsonrpc":"2.0","id":"2","method":"tools/list"}`)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("viewer (no mcp.invoke) calling tools/list status = %d, want 403", resp.StatusCode)
	}
}

func TestOversizedBodyRejected(t *testing.T) {
	env := newTestEnv(t)
	if _, err := bootstrap.Init(t.Context(), env.services.DB, env.services.UserStore, env.services.RoleStore, env.services.Users, "admin", testPassword, "Administrator"); err != nil {
		t.Fatalf("bootstrap.Init: %v", err)
	}
	token := env.login(t, "admin")

	huge := `{"jsonrpc":"2.0","id":"1","method":"ping","params":"` + strings.Repeat("x", 300*1024) + `"}`
	resp, _ := env.post(t, token, "", huge)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized body status = %d, want 413", resp.StatusCode)
	}
}

func TestCrossOriginRejected(t *testing.T) {
	env := newTestEnv(t)
	if _, err := bootstrap.Init(t.Context(), env.services.DB, env.services.UserStore, env.services.RoleStore, env.services.Users, "admin", testPassword, "Administrator"); err != nil {
		t.Fatalf("bootstrap.Init: %v", err)
	}
	token := env.login(t, "admin")

	req, _ := http.NewRequest(http.MethodPost, env.URL, bytes.NewBufferString(initializeRequest("1", mcp.ProtocolVersion)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Origin", "https://evil.example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-origin request status = %d, want 403", resp.StatusCode)
	}
}

func TestUnknownMethodReturnsMethodNotFound(t *testing.T) {
	env := newTestEnv(t)
	if _, err := bootstrap.Init(t.Context(), env.services.DB, env.services.UserStore, env.services.RoleStore, env.services.Users, "admin", testPassword, "Administrator"); err != nil {
		t.Fatalf("bootstrap.Init: %v", err)
	}
	token := env.login(t, "admin")
	sessionID := env.initializeSession(t, token)

	resp, parsed := env.post(t, token, sessionID, `{"jsonrpc":"2.0","id":"9","method":"not/a/real/method"}`)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("unknown method status = %d, want 200 (JSON-RPC error envelope, not a transport failure)", resp.StatusCode)
	}
	if parsed.Error == nil || parsed.Error.Code != mcp.ErrCodeMethodNotFound {
		t.Errorf("expected method-not-found error, got %+v", parsed.Error)
	}
}

func TestMissingSessionRejectedForNonInitializeMethods(t *testing.T) {
	env := newTestEnv(t)
	if _, err := bootstrap.Init(t.Context(), env.services.DB, env.services.UserStore, env.services.RoleStore, env.services.Users, "admin", testPassword, "Administrator"); err != nil {
		t.Fatalf("bootstrap.Init: %v", err)
	}
	token := env.login(t, "admin")

	resp, _ := env.post(t, token, "", `{"jsonrpc":"2.0","id":"1","method":"tools/list"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("tools/list without a session status = %d, want 400", resp.StatusCode)
	}

	resp, _ = env.post(t, token, "unknown-session-id", `{"jsonrpc":"2.0","id":"1","method":"tools/list"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("tools/list with an unknown session status = %d, want 400", resp.StatusCode)
	}
}

func TestDeleteTerminatesSession(t *testing.T) {
	env := newTestEnv(t)
	if _, err := bootstrap.Init(t.Context(), env.services.DB, env.services.UserStore, env.services.RoleStore, env.services.Users, "admin", testPassword, "Administrator"); err != nil {
		t.Fatalf("bootstrap.Init: %v", err)
	}
	token := env.login(t, "admin")
	sessionID := env.initializeSession(t, token)

	req, _ := http.NewRequest(http.MethodDelete, env.URL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(mcp.SessionIDHeader, sessionID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204", resp.StatusCode)
	}

	resp2, _ := env.post(t, token, sessionID, `{"jsonrpc":"2.0","id":"1","method":"tools/list"}`)
	if resp2.StatusCode != http.StatusBadRequest {
		t.Errorf("using a deleted session status = %d, want 400", resp2.StatusCode)
	}
}

func TestGetMethodNotAllowed(t *testing.T) {
	env := newTestEnv(t)
	resp, err := http.Get(env.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET /mcp status = %d, want 405", resp.StatusCode)
	}
}

// TestRESTAndMCPAuthorizationEquivalence proves the same principal gets the
// same allow/deny decision for mcp.invoke whether checked through the
// shared authz.Service directly or through the MCP transport, since both
// use the identical Service instance.
func TestRESTAndMCPAuthorizationEquivalence(t *testing.T) {
	env := newTestEnv(t)
	if _, err := bootstrap.Init(t.Context(), env.services.DB, env.services.UserStore, env.services.RoleStore, env.services.Users, "admin", testPassword, "Administrator"); err != nil {
		t.Fatalf("bootstrap.Init: %v", err)
	}
	env.createUserWithRole(t, "dev-user", roles.DeveloperRoleID)
	env.createUserWithRole(t, "viewer-user", roles.ViewerRoleID)

	for _, tc := range []struct {
		username    string
		wantAllowed bool
	}{
		{"dev-user", true},
		{"viewer-user", false},
	} {
		user, err := env.services.UserStore.GetByUsername(t.Context(), tc.username)
		if err != nil {
			t.Fatalf("looking up %s: %v", tc.username, err)
		}
		perms, err := env.services.Authz.EffectivePermissions(t.Context(), user.ID)
		if err != nil {
			t.Fatalf("EffectivePermissions: %v", err)
		}
		directDecision := perms[roles.PermMCPInvoke]
		if directDecision != tc.wantAllowed {
			t.Fatalf("authz.Service decision for %s = %v, want %v", tc.username, directDecision, tc.wantAllowed)
		}

		token := env.login(t, tc.username)
		sessionID := env.initializeSession(t, token)
		resp, _ := env.post(t, token, sessionID, `{"jsonrpc":"2.0","id":"1","method":"tools/list"}`)
		mcpAllowed := resp.StatusCode == http.StatusOK
		if mcpAllowed != directDecision {
			t.Errorf("MCP decision for %s = %v (status %d), want it to match authz.Service decision %v", tc.username, mcpAllowed, resp.StatusCode, directDecision)
		}
	}
}

func TestBuilderProfileListsDeveloperWorkflowTools(t *testing.T) {
	env := newTestEnvWithProfile(t, appliance.ProfileBuilder)
	if _, err := bootstrap.Init(t.Context(), env.services.DB, env.services.UserStore, env.services.RoleStore, env.services.Users, "admin", testPassword, "Administrator"); err != nil {
		t.Fatalf("bootstrap.Init: %v", err)
	}
	token := env.login(t, "admin")
	sessionID := env.initializeSession(t, token)

	resp, parsed := env.post(t, token, sessionID, `{"jsonrpc":"2.0","id":"1","method":"tools/list"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tools/list status = %d, want 200", resp.StatusCode)
	}
	var result struct {
		Tools []mcp.Tool `json:"tools"`
	}
	if err := json.Unmarshal(parsed.Result, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Tools) == 0 {
		t.Fatalf("builder profile should list developer workflow tools")
	}
	var sawCreateWorkspace, sawSubmitBuild bool
	for _, tool := range result.Tools {
		switch tool.Name {
		case "create_workspace":
			sawCreateWorkspace = strings.Contains(string(tool.InputSchema), "workspace_name")
		case "submit_build":
			sawSubmitBuild = strings.Contains(string(tool.InputSchema), "build_target")
		}
	}
	if !sawCreateWorkspace || !sawSubmitBuild {
		t.Fatalf("tools/list should expose explicit MCP schemas, got %+v", result.Tools)
	}
}

func TestCoreProfileHidesDeveloperWorkflowTools(t *testing.T) {
	env := newTestEnv(t)
	if _, err := bootstrap.Init(t.Context(), env.services.DB, env.services.UserStore, env.services.RoleStore, env.services.Users, "admin", testPassword, "Administrator"); err != nil {
		t.Fatalf("bootstrap.Init: %v", err)
	}
	token := env.login(t, "admin")
	sessionID := env.initializeSession(t, token)
	_, parsed := env.post(t, token, sessionID, `{"jsonrpc":"2.0","id":"1","method":"tools/list"}`)
	var result struct {
		Tools []mcp.Tool `json:"tools"`
	}
	if err := json.Unmarshal(parsed.Result, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Tools) != 0 {
		t.Fatalf("core profile tools = %+v, want none", result.Tools)
	}
}

func TestCoreProfileRejectsDirectDeveloperWorkflowToolCallAsNotFound(t *testing.T) {
	env := newTestEnv(t)
	if _, err := bootstrap.Init(t.Context(), env.services.DB, env.services.UserStore, env.services.RoleStore, env.services.Users, "admin", testPassword, "Administrator"); err != nil {
		t.Fatalf("bootstrap.Init: %v", err)
	}
	token := env.login(t, "admin")
	sessionID := env.initializeSession(t, token)

	resp, parsed := env.post(t, token, sessionID, `{"jsonrpc":"2.0","id":"1","method":"tools/call","params":{"name":"submit_build","arguments":{"build_target":"app"}}}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tools/call status = %d, want JSON-RPC 200", resp.StatusCode)
	}
	if parsed.Error == nil || parsed.Error.Code != mcp.ErrCodeMethodNotFound || parsed.Error.Message != "Tool not found" {
		t.Fatalf("core profile submit_build error = %+v, want tool-not-found", parsed.Error)
	}
}

func TestMCPInvokeAloneCannotSubmitBuild(t *testing.T) {
	env := newTestEnvWithProfile(t, appliance.ProfileBuilder)
	if _, err := bootstrap.Init(t.Context(), env.services.DB, env.services.UserStore, env.services.RoleStore, env.services.Users, "admin", testPassword, "Administrator"); err != nil {
		t.Fatalf("bootstrap.Init: %v", err)
	}
	actor := audit.Actor{Type: storage.AuditActorSystem, AuthMethod: "test"}
	role, err := env.services.Roles.Create(t.Context(), actor, "mcp-only", []string{roles.PermMCPInvoke})
	if err != nil {
		t.Fatalf("creating mcp-only role: %v", err)
	}
	user, err := env.services.Users.Create(t.Context(), actor, "mcp-only-user", "mcp-only-user", testPassword)
	if err != nil {
		t.Fatalf("creating user: %v", err)
	}
	if err := env.services.Roles.SetUserRoles(t.Context(), actor, user.ID, []string{role.ID}); err != nil {
		t.Fatal(err)
	}

	token := env.login(t, "mcp-only-user")
	sessionID := env.initializeSession(t, token)
	resp, parsed := env.post(t, token, sessionID, `{"jsonrpc":"2.0","id":"list","method":"tools/list"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tools/list status = %d, want 200", resp.StatusCode)
	}
	var listResult struct {
		Tools []mcp.Tool `json:"tools"`
	}
	if err := json.Unmarshal(parsed.Result, &listResult); err != nil {
		t.Fatalf("decoding tools/list result: %v", err)
	}
	if len(listResult.Tools) != 0 {
		t.Fatalf("mcp.invoke-only principal tools = %+v, want none", listResult.Tools)
	}

	resp, parsed = env.post(t, token, sessionID, `{"jsonrpc":"2.0","id":"1","method":"tools/call","params":{"name":"submit_build","arguments":{"build_target":"app"}}}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tools/call status = %d, want JSON-RPC 200", resp.StatusCode)
	}
	if parsed.Error == nil || parsed.Error.Code != mcp.ErrCodeInvalidRequest {
		t.Fatalf("submit_build with mcp.invoke only error = %+v, want permission denied invalid request", parsed.Error)
	}
}

func TestToolsListFiltersByToolPermission(t *testing.T) {
	env := newTestEnvWithProfile(t, appliance.ProfileBuilder)
	if _, err := bootstrap.Init(t.Context(), env.services.DB, env.services.UserStore, env.services.RoleStore, env.services.Users, "admin", testPassword, "Administrator"); err != nil {
		t.Fatalf("bootstrap.Init: %v", err)
	}
	actor := audit.Actor{Type: storage.AuditActorSystem, AuthMethod: "test"}
	role, err := env.services.Roles.Create(t.Context(), actor, "mcp-work-profiles-only", []string{roles.PermMCPInvoke, roles.PermWorkProfilesRead})
	if err != nil {
		t.Fatalf("creating role: %v", err)
	}
	user, err := env.services.Users.Create(t.Context(), actor, "mcp-work-profiles-user", "mcp-work-profiles-user", testPassword)
	if err != nil {
		t.Fatalf("creating user: %v", err)
	}
	if err := env.services.Roles.SetUserRoles(t.Context(), actor, user.ID, []string{role.ID}); err != nil {
		t.Fatal(err)
	}

	token := env.login(t, "mcp-work-profiles-user")
	sessionID := env.initializeSession(t, token)
	resp, parsed := env.post(t, token, sessionID, `{"jsonrpc":"2.0","id":"list","method":"tools/list"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tools/list status = %d, want 200", resp.StatusCode)
	}
	var result struct {
		Tools []mcp.Tool `json:"tools"`
	}
	if err := json.Unmarshal(parsed.Result, &result); err != nil {
		t.Fatalf("decoding tools/list result: %v", err)
	}
	if len(result.Tools) != 1 || result.Tools[0].Name != "list_work_profiles" {
		t.Fatalf("filtered tools = %+v, want only list_work_profiles", result.Tools)
	}

	resp, parsed = env.post(t, token, sessionID, `{"jsonrpc":"2.0","id":"submit","method":"tools/call","params":{"name":"submit_build","arguments":{"build_target":"app"}}}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tools/call status = %d, want JSON-RPC 200", resp.StatusCode)
	}
	if parsed.Error == nil || parsed.Error.Code != mcp.ErrCodeInvalidRequest {
		t.Fatalf("submit_build error = %+v, want permission denied invalid request", parsed.Error)
	}
}

func TestBuildPermissionAloneCannotUseMCP(t *testing.T) {
	env := newTestEnvWithProfile(t, appliance.ProfileBuilder)
	if _, err := bootstrap.Init(t.Context(), env.services.DB, env.services.UserStore, env.services.RoleStore, env.services.Users, "admin", testPassword, "Administrator"); err != nil {
		t.Fatalf("bootstrap.Init: %v", err)
	}
	env.createUserWithRole(t, "automation-user", roles.AutomationRoleID)
	token := env.login(t, "automation-user")
	sessionID := env.initializeSession(t, token)
	resp, _ := env.post(t, token, sessionID, `{"jsonrpc":"2.0","id":"1","method":"tools/list"}`)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("automation tools/list status = %d, want 403", resp.StatusCode)
	}
}

func TestBuilderProfileToolCallsSubmitStatusLogsAndCancelJob(t *testing.T) {
	env := newTestEnvWithProfile(t, appliance.ProfileBuilder)
	if _, err := bootstrap.Init(t.Context(), env.services.DB, env.services.UserStore, env.services.RoleStore, env.services.Users, "admin", testPassword, "Administrator"); err != nil {
		t.Fatalf("bootstrap.Init: %v", err)
	}
	fake, ok := env.services.WorkflowEngine.(*workflows.Fake)
	if !ok {
		t.Fatalf("workflow engine is %T, want *workflows.Fake", env.services.WorkflowEngine)
	}
	fake.Behavior = func(spec workflows.Spec) workflows.Status { return workflows.Status{Phase: workflows.PhaseRunning} }

	token := env.login(t, "admin")
	sessionID := env.initializeSession(t, token)

	callTool := func(id, name, args string) map[string]any {
		t.Helper()
		body := fmt.Sprintf(`{"jsonrpc":"2.0","id":%q,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, id, name, args)
		resp, parsed := env.post(t, token, sessionID, body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", name, resp.StatusCode)
		}
		if parsed.Error != nil {
			t.Fatalf("%s returned JSON-RPC error: %+v", name, parsed.Error)
		}
		var result struct {
			StructuredContent map[string]any `json:"structuredContent"`
		}
		if err := json.Unmarshal(parsed.Result, &result); err != nil {
			t.Fatalf("decoding %s result: %v", name, err)
		}
		return result.StructuredContent
	}

	created := callTool("create", "create_workspace", `{"workspace_name":"app","profile_name":"builder","repo_name":"app","source_ref":"0123456789abcdef0123456789abcdef01234567"}`)
	workspaceMap, ok := created["workspace"].(map[string]any)
	if !ok {
		t.Fatalf("create_workspace structured content = %+v, want workspace", created)
	}
	workspaceID, _ := workspaceMap["id"].(string)
	if workspaceID == "" {
		t.Fatalf("create_workspace workspace missing id: %+v", workspaceMap)
	}

	workspaces := callTool("workspaces", "list_workspaces", `{}`)
	if items, ok := workspaces["items"].([]any); !ok || len(items) == 0 {
		t.Fatalf("list_workspaces structured content = %+v, want items", workspaces)
	}

	current := callTool("current", "get_workspace", `{}`)
	if workspace, ok := current["workspace"].(map[string]any); !ok || workspace["id"] != workspaceID {
		t.Fatalf("get_workspace structured content = %+v, want current workspace %q", current, workspaceID)
	}

	selected := callTool("select", "set_workspace", fmt.Sprintf(`{"workspace_id":%q}`, workspaceID))
	if workspace, ok := selected["workspace"].(map[string]any); !ok || workspace["id"] != workspaceID {
		t.Fatalf("set_workspace structured content = %+v, want selected workspace %q", selected, workspaceID)
	}

	targets := callTool("targets", "list_build_targets", `{}`)
	if items, ok := targets["items"].([]any); !ok || len(items) == 0 {
		t.Fatalf("list_build_targets structured content = %+v, want items", targets)
	}

	submitted := callTool("submit", "submit_build", `{"build_target":"app","tag":"v1"}`)
	jobMap, ok := submitted["job"].(map[string]any)
	if !ok {
		t.Fatalf("submit_build structured content = %+v, want job", submitted)
	}
	jobID, _ := jobMap["id"].(string)
	if jobID == "" {
		t.Fatalf("submit_build job missing id: %+v", jobMap)
	}
	if artifactRef, _ := jobMap["artifactRef"].(string); artifactRef != "users/alice/app:v1" {
		t.Fatalf("submit_build artifactRef = %q, want users/alice/app:v1", artifactRef)
	}
	for _, secretText := range []string{"builder-git-key", "builder-git-known-hosts"} {
		if strings.Contains(fmt.Sprint(submitted), secretText) {
			t.Fatalf("submit_build structured content leaked source credential material %q: %+v", secretText, submitted)
		}
	}

	status := callTool("status", "get_job_status", fmt.Sprintf(`{"job_id":%q}`, jobID))
	if statusJob, ok := status["job"].(map[string]any); !ok || statusJob["status"] == "" {
		t.Fatalf("get_job_status structured content = %+v, want job status", status)
	} else if artifactRef, _ := statusJob["artifactRef"].(string); artifactRef != "users/alice/app:v1" {
		t.Fatalf("get_job_status artifactRef = %q, want users/alice/app:v1", artifactRef)
	}
	for _, secretText := range []string{"builder-git-key", "builder-git-known-hosts"} {
		if strings.Contains(fmt.Sprint(status), secretText) {
			t.Fatalf("get_job_status structured content leaked source credential material %q: %+v", secretText, status)
		}
	}

	steps := callTool("steps", "get_job_steps", fmt.Sprintf(`{"job_id":%q}`, jobID))
	if items, ok := steps["items"].([]any); !ok || len(items) == 0 {
		t.Fatalf("get_job_steps structured content = %+v, want items", steps)
	}

	logs := callTool("logs", "get_job_logs", fmt.Sprintf(`{"job_id":%q}`, jobID))
	if got, _ := logs["logs"].(string); !strings.Contains(got, "fake logs for workflow") {
		t.Fatalf("get_job_logs logs = %q, want fake workflow logs", got)
	}

	cancelled := callTool("cancel", "cancel_job", fmt.Sprintf(`{"job_id":%q}`, jobID))
	cancelledJob, ok := cancelled["job"].(map[string]any)
	if !ok {
		t.Fatalf("cancel_job structured content = %+v, want job", cancelled)
	}
	if cancelledJob["status"] != string(storage.JobStatusCancelled) {
		t.Fatalf("cancel_job status = %v, want cancelled", cancelledJob["status"])
	}
}

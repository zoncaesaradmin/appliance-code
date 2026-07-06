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
	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/bootstrap"
	"appliance-code/services/controlplane/internal/config"
	"appliance-code/services/controlplane/internal/mcp"
	"appliance-code/services/controlplane/internal/reqauth"
	"appliance-code/services/controlplane/internal/roles"
	"appliance-code/services/controlplane/internal/storage"
)

const canonicalOrigin = "https://appliance.example.internal"
const testPassword = "a-sufficiently-long-test-password-1"

type testEnv struct {
	*httptest.Server
	services *app.Services
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.CanonicalOrigin = canonicalOrigin

	services, err := app.WireServices(cfg)
	if err != nil {
		t.Fatalf("WireServices: %v", err)
	}
	t.Cleanup(func() { services.DB.Close() })

	deps := reqauth.Deps{Sessions: services.Sessions, Tokens: services.Tokens, Authz: services.Authz}
	handler := mcp.NewHandler(deps, cfg.CanonicalOrigin)

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

package applianceclient_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"appliance-code/server/sdk/golang/applianceclient"
)

func TestLoginSendsCredentialsAndParsesResult(t *testing.T) {
	var gotUsername, gotPassword string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/auth/login" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body struct{ Username, Password string }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		gotUsername, gotPassword = body.Username, body.Password

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"accessToken": "access-1", "refreshToken": "refresh-1", "accessExpiresAt": "2026-01-01T00:00:00Z",
		})
	}))
	defer srv.Close()

	client := applianceclient.New(srv.URL)
	result, err := client.Login(t.Context(), "admin", "hunter2-but-long-enough")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if gotUsername != "admin" || gotPassword != "hunter2-but-long-enough" {
		t.Errorf("server saw username=%q password=%q", gotUsername, gotPassword)
	}
	if result.AccessToken != "access-1" || result.RefreshToken != "refresh-1" {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestLoginReturnsProblemOnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type": "https://appliance.local/problems/invalid_credentials", "title": "Invalid username or password",
			"status": 401, "code": "invalid_credentials",
		})
	}))
	defer srv.Close()

	client := applianceclient.New(srv.URL)
	_, err := client.Login(t.Context(), "admin", "wrong-password")
	if err == nil {
		t.Fatal("expected an error")
	}
	problem, ok := err.(*applianceclient.Problem)
	if !ok {
		t.Fatalf("error type = %T, want *applianceclient.Problem", err)
	}
	if problem.Code != "invalid_credentials" || problem.Status != 401 {
		t.Errorf("unexpected problem: %+v", problem)
	}
}

func TestSessionSendsBearerToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"userId": "user-1", "authMethod": "session", "permissions": []string{"users.read"},
		})
	}))
	defer srv.Close()

	client := applianceclient.New(srv.URL)
	info, err := client.Session(t.Context(), "my-access-token")
	if err != nil {
		t.Fatalf("Session: %v", err)
	}
	if gotAuth != "Bearer my-access-token" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer my-access-token")
	}
	if info.UserID != "user-1" || len(info.Permissions) != 1 {
		t.Errorf("unexpected session info: %+v", info)
	}
}

func TestLogoutSendsNoBodyAndHandlesNoContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/auth/logout" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := applianceclient.New(srv.URL)
	if err := client.Logout(t.Context(), "token"); err != nil {
		t.Fatalf("Logout: %v", err)
	}
}

func TestCreateTokenParsesRawTokenAndMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body applianceclient.CreateTokenRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Name != "ci-token" {
			t.Errorf("request name = %q, want ci-token", body.Name)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token": "apt_raw-secret-value", "id": "tok-1", "userId": "user-1", "name": "ci-token",
			"createdAt": "2026-01-01T00:00:00Z", "expiresAt": "2026-04-01T00:00:00Z",
		})
	}))
	defer srv.Close()

	client := applianceclient.New(srv.URL)
	result, err := client.CreateToken(t.Context(), "access", applianceclient.CreateTokenRequest{Name: "ci-token"})
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	if result.Token != "apt_raw-secret-value" || result.ID != "tok-1" {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestRegistryTokenUsesBasicAuthAndScopeQuery(t *testing.T) {
	var gotUser, gotPass string
	var gotScopes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, _ = r.BasicAuth()
		gotScopes = r.URL.Query()["scope"]
		if r.URL.Query().Get("service") != "zot" {
			t.Errorf("service query = %q, want zot", r.URL.Query().Get("service"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token": "registry-jwt", "expires_in": 300, "issued_at": "2026-01-01T00:00:00Z",
		})
	}))
	defer srv.Close()

	client := applianceclient.New(srv.URL)
	result, err := client.RegistryToken(t.Context(), "alice", "apt_alice-token", "zot", []string{"repository:users/alice/app:pull,push"})
	if err != nil {
		t.Fatalf("RegistryToken: %v", err)
	}
	if gotUser != "alice" || gotPass != "apt_alice-token" {
		t.Errorf("basic auth = (%q, %q)", gotUser, gotPass)
	}
	if len(gotScopes) != 1 {
		t.Errorf("scopes = %v, want one entry", gotScopes)
	}
	if result.Token != "registry-jwt" || result.ExpiresIn != 300 {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestRevokeTokenEscapesPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// EscapedPath (not the decoded Path) is what shows whether the "/"
		// was actually sent on the wire as %2F rather than a literal path
		// separator that a router could misinterpret as an extra segment.
		gotPath = r.URL.EscapedPath()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := applianceclient.New(srv.URL)
	if err := client.RevokeToken(t.Context(), "access", "tok/with-slash"); err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}
	if gotPath != "/api/v1/tokens/tok%2Fwith-slash" {
		t.Errorf("escaped path = %q, want escaped token id", gotPath)
	}
}

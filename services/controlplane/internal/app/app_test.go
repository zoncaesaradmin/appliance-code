package app_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"appliance-code/services/controlplane/internal/app"
	"appliance-code/services/controlplane/internal/appliance"
	"appliance-code/services/controlplane/internal/config"
	"appliance-code/services/controlplane/internal/devflows"
	"appliance-code/services/controlplane/internal/logging"
	"appliance-code/services/controlplane/internal/storage"
)

// freeAddr asks the OS for an available loopback port and returns it as an
// addr string, closing the listener immediately so the caller can bind it.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding a free port: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("closing probe listener: %v", err)
	}
	return addr
}

func testConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PublicAddr = freeAddr(t)
	cfg.InternalAddr = freeAddr(t)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("test config invalid: %v", err)
	}
	return cfg
}

func TestAppStartsServesHealthAndShutsDownCleanly(t *testing.T) {
	cfg := testConfig(t)
	logger, err := logging.New("error")
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}

	a, err := app.New(cfg, logger, logger)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	waitForListener(t, cfg.InternalAddr)

	assertStatus(t, "http://"+cfg.InternalAddr+"/health/live", http.StatusOK)
	assertStatus(t, "http://"+cfg.InternalAddr+"/health/ready", http.StatusOK)
	assertStatus(t, "http://"+cfg.InternalAddr+"/health/startup", http.StatusOK)
	assertStatus(t, "http://"+cfg.PublicAddr+"/nonexistent", http.StatusNotFound)

	resp, err := http.Get("http://" + cfg.InternalAddr + "/version")
	if err != nil {
		t.Fatalf("GET /version: %v", err)
	}
	defer resp.Body.Close()
	var v struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decoding /version: %v", err)
	}
	if v.Version == "" {
		t.Error("/version returned empty version")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned error after shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within 5s of shutdown signal")
	}
}

func TestAppNewRequiresLoggers(t *testing.T) {
	cfg := testConfig(t)
	logger, err := logging.New("error")
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}

	if _, err := app.New(cfg, nil, logger); err == nil {
		t.Fatal("app.New should reject nil application logger")
	}
	if _, err := app.New(cfg, logger, nil); err == nil {
		t.Fatal("app.New should reject nil process logger")
	}
}

func TestWireServicesReconcilesBuildAndJobStateOnStartup(t *testing.T) {
	cfg := testConfig(t)
	cfg.ApplianceProfile = string(appliance.ProfileBuilder)
	cfg.WorkflowEngine = "fake"
	cfg.WorkspaceProvisionerImageDigest = "workspace-provisioner@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	cfg.BuilderImageDigest = "buildah@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	cfg.BuildCatalog = devflows.Catalog{
		WorkProfiles: []devflows.WorkProfile{{Name: "builder", Repos: []devflows.ProfileRepo{{Name: "app", EnabledByDefault: true}}}},
		Repos:        []devflows.Repo{{Name: "app", URL: "https://git.internal.example.com/team/app", DefaultRef: "0123456789abcdef0123456789abcdef01234567"}},
		BuildTargets: []devflows.BuildTarget{{
			Name: "default", Repo: "app", Execution: devflows.ExecutionScript,
			ImageRepository: "users/alice/app",
		}},
	}

	logger, err := logging.New("error")
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}
	services, err := app.WireServices(cfg, logger)
	if err != nil {
		t.Fatalf("initial WireServices: %v", err)
	}
	now := time.Now().UTC()
	user := storage.User{ID: "user-reconcile", Username: "alice", DisplayName: "Alice", State: storage.UserStateActive, CreatedAt: now, UpdatedAt: now}
	if err := services.UserStore.Create(t.Context(), user); err != nil {
		t.Fatalf("Create user: %v", err)
	}
	build := storage.Build{
		ID: "build-reconcile", OwnerID: user.ID, Status: storage.BuildStatusRunning,
		SourceRepoURL: "https://git.internal.example.com/team/app", SourceCommitSHA: "0123456789abcdef0123456789abcdef01234567",
		ContainerfilePath: "Containerfile", ImageRepository: "users/alice/app", ImageTag: "v1",
		BuilderImageDigest: "buildah@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", WorkflowName: "build-reconcile-workflow",
		CreatedAt: now, UpdatedAt: now, StartedAt: &now, DeadlineAt: now.Add(-time.Minute),
	}
	if err := services.BuildStore.Create(t.Context(), build); err != nil {
		t.Fatalf("Create build: %v", err)
	}
	job := storage.Job{
		ID: "job-reconcile", OwnerID: user.ID, BuildID: build.ID, Type: storage.JobTypeBuild,
		Status: storage.JobStatusRunning, TargetName: "default", CreatedAt: now, UpdatedAt: now, StartedAt: &now,
	}
	if err := services.JobStore.Create(t.Context(), job); err != nil {
		t.Fatalf("Create job: %v", err)
	}
	if err := services.DB.Close(); err != nil {
		t.Fatalf("closing first services DB: %v", err)
	}

	restarted, err := app.WireServices(cfg, logger)
	if err != nil {
		t.Fatalf("restart WireServices: %v", err)
	}
	defer restarted.DB.Close()

	reconciledBuild, err := restarted.BuildStore.Get(t.Context(), build.ID)
	if err != nil {
		t.Fatalf("Get build: %v", err)
	}
	if reconciledBuild.Status != storage.BuildStatusTimedOut || reconciledBuild.ReasonCode != "deadline_exceeded" {
		t.Fatalf("build after startup reconciliation = status %q reason %q, want timed_out/deadline_exceeded", reconciledBuild.Status, reconciledBuild.ReasonCode)
	}
	reconciledJob, err := restarted.JobStore.Get(t.Context(), job.ID)
	if err != nil {
		t.Fatalf("Get job: %v", err)
	}
	if reconciledJob.Status != storage.JobStatusFailed || reconciledJob.ReasonCode != "deadline_exceeded" {
		t.Fatalf("job after startup reconciliation = status %q reason %q, want failed/deadline_exceeded", reconciledJob.Status, reconciledJob.ReasonCode)
	}
}

func TestStorageProfileReadinessAcceptsRegistryAuthChallenge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	cfg := testConfig(t)
	cfg.ApplianceProfile = string(appliance.ProfileStorage)
	cfg.ZotBaseURL = srv.URL
	cfg.ZotAllowFake = false
	logger, err := logging.New("error")
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}

	a, err := app.New(cfg, logger, logger)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	waitForListener(t, cfg.InternalAddr)

	assertStatus(t, "http://"+cfg.InternalAddr+"/health/ready", http.StatusOK)

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within 5s of shutdown signal")
	}
}

func TestStorageProfileRegistryClientUsesInternalBearerAuth(t *testing.T) {
	var seenAuthCatalog, seenAuthTags string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/_catalog":
			seenAuthCatalog = r.Header.Get("Authorization")
			if seenAuthCatalog == "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"repositories": []string{"users/alice/app"}})
		case "/v2/users/alice/app/tags/list":
			seenAuthTags = r.Header.Get("Authorization")
			if seenAuthTags == "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "users/alice/app", "tags": []string{"v1"}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	cfg := testConfig(t)
	cfg.ApplianceProfile = string(appliance.ProfileStorage)
	cfg.ZotBaseURL = srv.URL
	cfg.ZotAllowFake = false
	logger, err := logging.New("error")
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}

	services, err := app.WireServices(cfg, logger)
	if err != nil {
		t.Fatalf("WireServices: %v", err)
	}
	defer services.DB.Close()

	repos, err := services.Zot.ListRepositories(t.Context())
	if err != nil {
		t.Fatalf("ListRepositories: %v", err)
	}
	if len(repos) != 1 || repos[0] != "users/alice/app" {
		t.Fatalf("repositories = %v", repos)
	}
	tags, err := services.Zot.ListTags(t.Context(), "users/alice/app")
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	if len(tags) != 1 || tags[0] != "v1" {
		t.Fatalf("tags = %v", tags)
	}
	if seenAuthCatalog == "" || seenAuthTags == "" {
		t.Fatalf("expected bearer auth on zot requests, got catalog=%q tags=%q", seenAuthCatalog, seenAuthTags)
	}
	catalogClaims := decodeBearerClaims(t, seenAuthCatalog)
	if access := catalogClaims["access"].([]any); len(access) != 1 {
		t.Fatalf("catalog access = %v", access)
	} else {
		entry := access[0].(map[string]any)
		if entry["type"] != "registry" || entry["name"] != "catalog" {
			t.Fatalf("catalog access entry = %v", entry)
		}
	}
	tagsClaims := decodeBearerClaims(t, seenAuthTags)
	if access := tagsClaims["access"].([]any); len(access) != 1 {
		t.Fatalf("tags access = %v", access)
	} else {
		entry := access[0].(map[string]any)
		if entry["type"] != "repository" || entry["name"] != "users/alice/app" {
			t.Fatalf("tags access entry = %v", entry)
		}
	}
}

func decodeBearerClaims(t *testing.T, auth string) map[string]any {
	t.Helper()
	raw := strings.TrimPrefix(auth, "Bearer ")
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		t.Fatalf("invalid bearer token %q", auth)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode bearer payload: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("unmarshal bearer payload: %v", err)
	}
	return claims
}

func waitForListener(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("listener at %s did not come up in time", addr)
}

func assertStatus(t *testing.T, url string, want int) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) //nolint:errcheck
	if resp.StatusCode != want {
		t.Errorf("GET %s status = %d, want %d", url, resp.StatusCode, want)
	}
}

package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"appliance-code/server/backend/internal/roles"
	"appliance-code/server/backend/internal/workflows"
)

func (ts *testServer) fakeWorkflowEngine(t *testing.T) *workflows.Fake {
	t.Helper()
	fake, ok := ts.services.WorkflowEngine.(*workflows.Fake)
	if !ok {
		t.Fatalf("services.WorkflowEngine is %T, want *workflows.Fake in tests", ts.services.WorkflowEngine)
	}
	return fake
}

const validCreateBuildBody = `{
	"sourceRepoUrl": "https://git.internal.example.com/team/app",
	"sourceCommitSha": "0123456789abcdef0123456789abcdef01234567",
	"imageRepository": "users/%s/app",
	"imageTag": "v1",
	"builderImageDigest": "buildah@sha256:approved"
}`

func TestCreateBuildHappyPath(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)
	ts.createUserWithRole(t, "alice", testPassword, roles.DeveloperRoleID)
	token := ts.login(t, "alice", testPassword)

	body := fmt.Sprintf(validCreateBuildBody, "alice")
	resp := ts.doJSON(t, "POST", "/api/v1/builds", token, body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var build struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&build); err != nil {
		t.Fatal(err)
	}
	if build.Status != "running" {
		t.Errorf("status = %q, want running", build.Status)
	}
}

func TestCreateBuildRejectsMaliciousSource(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)
	ts.createUserWithRole(t, "alice", testPassword, roles.DeveloperRoleID)
	token := ts.login(t, "alice", testPassword)

	body := `{
		"sourceRepoUrl": "https://evil.example.com/team/app",
		"sourceCommitSha": "0123456789abcdef0123456789abcdef01234567",
		"imageRepository": "users/alice/app",
		"imageTag": "v1",
		"builderImageDigest": "buildah@sha256:approved"
	}`
	resp := ts.doJSON(t, "POST", "/api/v1/builds", token, body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status for disallowed source host = %d, want 400", resp.StatusCode)
	}
}

func TestCreateBuildIdempotencyKey(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)
	ts.createUserWithRole(t, "alice", testPassword, roles.DeveloperRoleID)
	token := ts.login(t, "alice", testPassword)

	body := fmt.Sprintf(validCreateBuildBody, "alice")
	first := ts.doJSONWithHeaders(t, "POST", "/api/v1/builds", token, body, map[string]string{"Idempotency-Key": "key-1"})
	defer first.Body.Close()
	var firstBuild struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(first.Body).Decode(&firstBuild); err != nil {
		t.Fatal(err)
	}

	second := ts.doJSONWithHeaders(t, "POST", "/api/v1/builds", token, body, map[string]string{"Idempotency-Key": "key-1"})
	defer second.Body.Close()
	var secondBuild struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(second.Body).Decode(&secondBuild); err != nil {
		t.Fatal(err)
	}
	if firstBuild.ID != secondBuild.ID {
		t.Errorf("duplicate create with the same idempotency key returned a different build: %s vs %s", firstBuild.ID, secondBuild.ID)
	}
}

func TestBuildOwnershipEnforced(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)
	ts.createUserWithRole(t, "alice", testPassword, roles.DeveloperRoleID)
	ts.createUserWithRole(t, "bob", testPassword, roles.DeveloperRoleID)
	aliceToken := ts.login(t, "alice", testPassword)
	bobToken := ts.login(t, "bob", testPassword)

	createResp := ts.doJSON(t, "POST", "/api/v1/builds", aliceToken, fmt.Sprintf(validCreateBuildBody, "alice"))
	defer createResp.Body.Close()
	var build struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&build); err != nil {
		t.Fatal(err)
	}

	// Bob (developer, no builds.read.any) must not be able to see Alice's
	// build; the response should be 404, not 403, to avoid confirming it
	// exists at all.
	resp := ts.doJSON(t, "GET", "/api/v1/builds/"+build.ID, bobToken, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("bob reading alice's build status = %d, want 404", resp.StatusCode)
	}

	// Alice can read her own build.
	ownResp := ts.doJSON(t, "GET", "/api/v1/builds/"+build.ID, aliceToken, "")
	defer ownResp.Body.Close()
	if ownResp.StatusCode != http.StatusOK {
		t.Errorf("alice reading her own build status = %d, want 200", ownResp.StatusCode)
	}
}

func TestAdminCanReadAnyBuild(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)
	ts.createUserWithRole(t, "alice", testPassword, roles.DeveloperRoleID)
	aliceToken := ts.login(t, "alice", testPassword)
	adminToken := ts.login(t, "admin", testPassword)

	createResp := ts.doJSON(t, "POST", "/api/v1/builds", aliceToken, fmt.Sprintf(validCreateBuildBody, "alice"))
	defer createResp.Body.Close()
	var build struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&build); err != nil {
		t.Fatal(err)
	}

	resp := ts.doJSON(t, "GET", "/api/v1/builds/"+build.ID, adminToken, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("administrator reading any build status = %d, want 200", resp.StatusCode)
	}
}

func TestCancelBuild(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)
	ts.createUserWithRole(t, "alice", testPassword, roles.DeveloperRoleID)
	token := ts.login(t, "alice", testPassword)

	fake := ts.fakeWorkflowEngine(t)
	fake.Behavior = func(spec workflows.Spec) workflows.Status { return workflows.Status{Phase: workflows.PhaseRunning} }

	createResp := ts.doJSON(t, "POST", "/api/v1/builds", token, fmt.Sprintf(validCreateBuildBody, "alice"))
	defer createResp.Body.Close()
	var build struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&build); err != nil {
		t.Fatal(err)
	}

	resp := ts.doJSON(t, "POST", "/api/v1/builds/"+build.ID+"/cancel", token, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel status = %d, want 200", resp.StatusCode)
	}
	var cancelled struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&cancelled); err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != "cancelled" {
		t.Errorf("status after cancel = %q, want cancelled", cancelled.Status)
	}
}

func TestListBuildsFiltersByOwnerForDeveloper(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)
	ts.createUserWithRole(t, "alice", testPassword, roles.DeveloperRoleID)
	ts.createUserWithRole(t, "bob", testPassword, roles.DeveloperRoleID)
	aliceToken := ts.login(t, "alice", testPassword)
	bobToken := ts.login(t, "bob", testPassword)

	for _, r := range []struct{ token, owner string }{{aliceToken, "alice"}, {bobToken, "bob"}} {
		resp := ts.doJSON(t, "POST", "/api/v1/builds", r.token, fmt.Sprintf(validCreateBuildBody, r.owner))
		resp.Body.Close()
	}

	listResp := ts.doJSON(t, "GET", "/api/v1/builds", aliceToken, "")
	defer listResp.Body.Close()
	var list struct {
		Items []struct {
			OwnerID string `json:"ownerId"`
		} `json:"items"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("alice's build list = %+v, want exactly her own build", list.Items)
	}
}

package httpapi_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"appliance-code/services/controlplane/internal/appliance"
	"appliance-code/services/controlplane/internal/roles"
	"appliance-code/services/controlplane/internal/workflows"
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
		ID          string `json:"id"`
		Status      string `json:"status"`
		ArtifactRef string `json:"artifactRef"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&build); err != nil {
		t.Fatal(err)
	}
	if build.Status != "running" {
		t.Errorf("status = %q, want running", build.Status)
	}
	if build.ArtifactRef != "users/alice/app:v1" {
		t.Errorf("artifactRef = %q, want users/alice/app:v1", build.ArtifactRef)
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

func TestDeveloperWorkflowSubmitBuildByCurrentWorkspace(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)
	ts.createUserWithRole(t, "alice", testPassword, roles.DeveloperRoleID)
	token := ts.login(t, "alice", testPassword)
	fake := ts.fakeWorkflowEngine(t)
	fake.Behavior = func(spec workflows.Spec) workflows.Status { return workflows.Status{Phase: workflows.PhaseRunning} }

	createWorkspace := ts.doJSON(t, "POST", "/api/v1/workspaces", token, `{"name":"app","workProfile":"builder","repo":"app","sourceRef":"0123456789abcdef0123456789abcdef01234567"}`)
	defer createWorkspace.Body.Close()
	if createWorkspace.StatusCode != http.StatusCreated {
		t.Fatalf("create workspace status = %d, want 201", createWorkspace.StatusCode)
	}

	targets := ts.doJSON(t, "GET", "/api/v1/current-workspace/build-targets", token, "")
	defer targets.Body.Close()
	if targets.StatusCode != http.StatusOK {
		t.Fatalf("list build targets status = %d, want 200", targets.StatusCode)
	}

	submit := ts.doJSON(t, "POST", "/api/v1/current-workspace/builds", token, `{"targetName":"app","imageTag":"v1"}`)
	defer submit.Body.Close()
	if submit.StatusCode != http.StatusCreated {
		t.Fatalf("submit current build status = %d, want 201", submit.StatusCode)
	}
	var job struct {
		ID          string `json:"id"`
		BuildID     string `json:"buildId"`
		Status      string `json:"status"`
		ArtifactRef string `json:"artifactRef"`
	}
	if err := json.NewDecoder(submit.Body).Decode(&job); err != nil {
		t.Fatal(err)
	}
	if job.ID == "" || job.BuildID == "" || job.Status == "" {
		t.Fatalf("job response missing required fields: %+v", job)
	}
	if job.ArtifactRef != "users/alice/app:v1" {
		t.Fatalf("job artifactRef = %q, want users/alice/app:v1", job.ArtifactRef)
	}
	submittedSpec, ok := fake.SubmittedSpec("build-" + job.BuildID)
	if !ok {
		t.Fatalf("workflow spec for build %s was not submitted", job.BuildID)
	}
	if submittedSpec.SourceCredentialRef != "git-main" || submittedSpec.SourceCredentialSecret != "git-main-key" || submittedSpec.KnownHostsSecret != "git-known-hosts" {
		t.Fatalf("workflow spec did not receive expected source credential secret refs: %+v", submittedSpec)
	}
	if submittedSpec.Execution != "repo_script" || submittedSpec.ScriptPath != "build.sh" {
		t.Fatalf("workflow spec did not receive expected target execution fields: %+v", submittedSpec)
	}

	status := ts.doJSON(t, "GET", "/api/v1/jobs/"+job.ID, token, "")
	if status.StatusCode != http.StatusOK {
		status.Body.Close()
		t.Fatalf("job status status = %d, want 200", status.StatusCode)
	}
	statusBody, err := io.ReadAll(status.Body)
	status.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	for _, secretText := range []string{"git-main-key", "git-known-hosts"} {
		if strings.Contains(string(statusBody), secretText) {
			t.Fatalf("job status response leaked source credential material %q: %s", secretText, string(statusBody))
		}
	}

	currentStatus := ts.doJSON(t, "GET", "/api/v1/current-workspace/build-status", token, "")
	defer currentStatus.Body.Close()
	if currentStatus.StatusCode != http.StatusOK {
		t.Fatalf("current workspace build status = %d, want 200", currentStatus.StatusCode)
	}
	var currentJob struct {
		ID          string `json:"id"`
		ArtifactRef string `json:"artifactRef"`
	}
	if err := json.NewDecoder(currentStatus.Body).Decode(&currentJob); err != nil {
		t.Fatal(err)
	}
	if currentJob.ID != job.ID {
		t.Fatalf("current workspace build status returned job %q, want latest job %q", currentJob.ID, job.ID)
	}
	if currentJob.ArtifactRef != job.ArtifactRef {
		t.Fatalf("current workspace build status artifactRef = %q, want %q", currentJob.ArtifactRef, job.ArtifactRef)
	}

	steps := ts.doJSON(t, "GET", "/api/v1/jobs/"+job.ID+"/steps", token, "")
	defer steps.Body.Close()
	if steps.StatusCode != http.StatusOK {
		t.Fatalf("job steps status = %d, want 200", steps.StatusCode)
	}
	var stepList struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
	}
	if err := json.NewDecoder(steps.Body).Decode(&stepList); err != nil {
		t.Fatal(err)
	}
	if len(stepList.Items) == 0 || stepList.Items[0].Name != "submit-build-workflow" {
		t.Fatalf("job steps = %+v, want submit-build-workflow", stepList.Items)
	}

	logs := ts.doJSON(t, "GET", "/api/v1/jobs/"+job.ID+"/logs", token, "")
	defer logs.Body.Close()
	if logs.StatusCode != http.StatusOK {
		t.Fatalf("job logs status = %d, want 200", logs.StatusCode)
	}

	cancel := ts.doJSON(t, "POST", "/api/v1/jobs/"+job.ID+"/cancel", token, "")
	defer cancel.Body.Close()
	if cancel.StatusCode != http.StatusOK {
		t.Fatalf("job cancel status = %d, want 200", cancel.StatusCode)
	}
	var cancelled struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(cancel.Body).Decode(&cancelled); err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != "cancelled" {
		t.Fatalf("job status after cancel = %q, want cancelled", cancelled.Status)
	}

	cancelAgain := ts.doJSON(t, "POST", "/api/v1/jobs/"+job.ID+"/cancel", token, "")
	defer cancelAgain.Body.Close()
	if cancelAgain.StatusCode != http.StatusOK {
		t.Fatalf("second job cancel status = %d, want 200", cancelAgain.StatusCode)
	}
	var cancelledAgain struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(cancelAgain.Body).Decode(&cancelledAgain); err != nil {
		t.Fatal(err)
	}
	if cancelledAgain.Status != "cancelled" {
		t.Fatalf("job status after second cancel = %q, want cancelled", cancelledAgain.Status)
	}
}

func TestCurrentWorkspaceBuildIdempotencyKey(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)
	ts.createUserWithRole(t, "alice", testPassword, roles.DeveloperRoleID)
	token := ts.login(t, "alice", testPassword)

	createWorkspace := ts.doJSON(t, "POST", "/api/v1/workspaces", token, `{"name":"app","workProfile":"builder","repo":"app","sourceRef":"0123456789abcdef0123456789abcdef01234567"}`)
	defer createWorkspace.Body.Close()
	if createWorkspace.StatusCode != http.StatusCreated {
		t.Fatalf("create workspace status = %d, want 201", createWorkspace.StatusCode)
	}

	body := `{"targetName":"app","imageTag":"v1"}`
	first := ts.doJSONWithHeaders(t, "POST", "/api/v1/current-workspace/builds", token, body, map[string]string{"Idempotency-Key": "job-key-1"})
	defer first.Body.Close()
	if first.StatusCode != http.StatusCreated {
		t.Fatalf("first submit status = %d, want 201", first.StatusCode)
	}
	var firstJob struct {
		ID      string `json:"id"`
		BuildID string `json:"buildId"`
	}
	if err := json.NewDecoder(first.Body).Decode(&firstJob); err != nil {
		t.Fatal(err)
	}

	second := ts.doJSONWithHeaders(t, "POST", "/api/v1/current-workspace/builds", token, body, map[string]string{"Idempotency-Key": "job-key-1"})
	defer second.Body.Close()
	if second.StatusCode != http.StatusCreated {
		t.Fatalf("second submit status = %d, want 201", second.StatusCode)
	}
	var secondJob struct {
		ID      string `json:"id"`
		BuildID string `json:"buildId"`
	}
	if err := json.NewDecoder(second.Body).Decode(&secondJob); err != nil {
		t.Fatal(err)
	}
	if secondJob.ID != firstJob.ID || secondJob.BuildID != firstJob.BuildID {
		t.Fatalf("idempotency replay returned job/build %s/%s, want %s/%s", secondJob.ID, secondJob.BuildID, firstJob.ID, firstJob.BuildID)
	}

	conflict := ts.doJSONWithHeaders(t, "POST", "/api/v1/current-workspace/builds", token, `{"targetName":"app","imageTag":"v2"}`, map[string]string{"Idempotency-Key": "job-key-1"})
	defer conflict.Body.Close()
	if conflict.StatusCode != http.StatusConflict {
		t.Fatalf("reused key with different body status = %d, want 409", conflict.StatusCode)
	}
}

func TestCurrentWorkspaceBuildRejectsUnknownTarget(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)
	ts.createUserWithRole(t, "alice", testPassword, roles.DeveloperRoleID)
	token := ts.login(t, "alice", testPassword)

	createWorkspace := ts.doJSON(t, "POST", "/api/v1/workspaces", token, `{"name":"app","workProfile":"builder","repo":"app","sourceRef":"0123456789abcdef0123456789abcdef01234567"}`)
	defer createWorkspace.Body.Close()
	if createWorkspace.StatusCode != http.StatusCreated {
		t.Fatalf("create workspace status = %d, want 201", createWorkspace.StatusCode)
	}
	submit := ts.doJSON(t, "POST", "/api/v1/current-workspace/builds", token, `{"targetName":"does-not-exist","imageTag":"v1"}`)
	defer submit.Body.Close()
	if submit.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown target submit status = %d, want 400", submit.StatusCode)
	}
}

func TestCurrentWorkspaceBuildRejectsMutableSourceRef(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)
	ts.createUserWithRole(t, "alice", testPassword, roles.DeveloperRoleID)
	token := ts.login(t, "alice", testPassword)

	createWorkspace := ts.doJSON(t, "POST", "/api/v1/workspaces", token, `{"name":"app","workProfile":"builder","repo":"app","sourceRef":"main"}`)
	defer createWorkspace.Body.Close()
	if createWorkspace.StatusCode != http.StatusCreated {
		t.Fatalf("create workspace status = %d, want 201", createWorkspace.StatusCode)
	}
	submit := ts.doJSON(t, "POST", "/api/v1/current-workspace/builds", token, `{"targetName":"app","imageTag":"v1"}`)
	defer submit.Body.Close()
	if submit.StatusCode != http.StatusBadRequest {
		t.Fatalf("mutable source ref submit status = %d, want 400", submit.StatusCode)
	}
}

func TestJobVisibilityAndCancelHonorSelfAndAnyPermissions(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)
	ts.createUserWithRole(t, "alice", testPassword, roles.DeveloperRoleID)
	ts.createUserWithRole(t, "bob", testPassword, roles.DeveloperRoleID)
	aliceToken := ts.login(t, "alice", testPassword)
	bobToken := ts.login(t, "bob", testPassword)
	adminToken := ts.login(t, "admin", testPassword)
	fake := ts.fakeWorkflowEngine(t)
	fake.Behavior = func(spec workflows.Spec) workflows.Status { return workflows.Status{Phase: workflows.PhaseRunning} }

	createWorkspace := ts.doJSON(t, "POST", "/api/v1/workspaces", aliceToken, `{"name":"app","workProfile":"builder","repo":"app","sourceRef":"0123456789abcdef0123456789abcdef01234567"}`)
	defer createWorkspace.Body.Close()
	if createWorkspace.StatusCode != http.StatusCreated {
		t.Fatalf("create workspace status = %d, want 201", createWorkspace.StatusCode)
	}
	submit := ts.doJSON(t, "POST", "/api/v1/current-workspace/builds", aliceToken, `{"targetName":"app","imageTag":"v1"}`)
	defer submit.Body.Close()
	if submit.StatusCode != http.StatusCreated {
		t.Fatalf("submit current build status = %d, want 201", submit.StatusCode)
	}
	var job struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(submit.Body).Decode(&job); err != nil {
		t.Fatal(err)
	}

	bobList := ts.doJSON(t, "GET", "/api/v1/jobs", bobToken, "")
	defer bobList.Body.Close()
	if bobList.StatusCode != http.StatusOK {
		t.Fatalf("bob list jobs status = %d, want 200", bobList.StatusCode)
	}
	var bobJobs struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.NewDecoder(bobList.Body).Decode(&bobJobs); err != nil {
		t.Fatal(err)
	}
	for _, item := range bobJobs.Items {
		if item.ID == job.ID {
			t.Fatalf("bob job list exposed alice job %q", job.ID)
		}
	}

	bobGet := ts.doJSON(t, "GET", "/api/v1/jobs/"+job.ID, bobToken, "")
	defer bobGet.Body.Close()
	if bobGet.StatusCode != http.StatusNotFound {
		t.Fatalf("bob get alice job status = %d, want 404", bobGet.StatusCode)
	}
	bobCancel := ts.doJSON(t, "POST", "/api/v1/jobs/"+job.ID+"/cancel", bobToken, "")
	defer bobCancel.Body.Close()
	if bobCancel.StatusCode != http.StatusNotFound {
		t.Fatalf("bob cancel alice job status = %d, want 404", bobCancel.StatusCode)
	}

	adminGet := ts.doJSON(t, "GET", "/api/v1/jobs/"+job.ID, adminToken, "")
	defer adminGet.Body.Close()
	if adminGet.StatusCode != http.StatusOK {
		t.Fatalf("admin get alice job status = %d, want 200", adminGet.StatusCode)
	}
	adminCancel := ts.doJSON(t, "POST", "/api/v1/jobs/"+job.ID+"/cancel", adminToken, "")
	defer adminCancel.Body.Close()
	if adminCancel.StatusCode != http.StatusOK {
		t.Fatalf("admin cancel alice job status = %d, want 200", adminCancel.StatusCode)
	}
}

func TestWorkspaceListHonorsReadAnyPermission(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)
	ts.createUserWithRole(t, "alice", testPassword, roles.DeveloperRoleID)
	ts.createUserWithRole(t, "bob", testPassword, roles.DeveloperRoleID)
	aliceToken := ts.login(t, "alice", testPassword)
	bobToken := ts.login(t, "bob", testPassword)
	adminToken := ts.login(t, "admin", testPassword)

	for _, tc := range []struct {
		token string
		name  string
	}{
		{aliceToken, "alice-app"},
		{bobToken, "bob-app"},
	} {
		resp := ts.doJSON(t, "POST", "/api/v1/workspaces", tc.token, fmt.Sprintf(`{"name":%q,"workProfile":"builder","repo":"app","sourceRef":"0123456789abcdef0123456789abcdef01234567"}`, tc.name))
		resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create workspace %s status = %d, want 201", tc.name, resp.StatusCode)
		}
	}

	aliceList := ts.doJSON(t, "GET", "/api/v1/workspaces", aliceToken, "")
	defer aliceList.Body.Close()
	var aliceBody struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
	}
	if err := json.NewDecoder(aliceList.Body).Decode(&aliceBody); err != nil {
		t.Fatal(err)
	}
	if len(aliceBody.Items) != 1 || aliceBody.Items[0].Name != "alice-app" {
		t.Fatalf("developer workspace list = %+v, want only alice-app", aliceBody.Items)
	}

	adminList := ts.doJSON(t, "GET", "/api/v1/workspaces", adminToken, "")
	defer adminList.Body.Close()
	var adminBody struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
	}
	if err := json.NewDecoder(adminList.Body).Decode(&adminBody); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, item := range adminBody.Items {
		seen[item.Name] = true
	}
	if !seen["alice-app"] || !seen["bob-app"] {
		t.Fatalf("admin workspace list = %+v, want alice-app and bob-app", adminBody.Items)
	}
}

func TestDeleteWorkspaceRejectsActiveJobs(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)
	ts.createUserWithRole(t, "alice", testPassword, roles.DeveloperRoleID)
	token := ts.login(t, "alice", testPassword)
	fake := ts.fakeWorkflowEngine(t)
	fake.Behavior = func(spec workflows.Spec) workflows.Status { return workflows.Status{Phase: workflows.PhaseRunning} }

	createWorkspace := ts.doJSON(t, "POST", "/api/v1/workspaces", token, `{"name":"app","workProfile":"builder","repo":"app","sourceRef":"0123456789abcdef0123456789abcdef01234567"}`)
	defer createWorkspace.Body.Close()
	if createWorkspace.StatusCode != http.StatusCreated {
		t.Fatalf("create workspace status = %d, want 201", createWorkspace.StatusCode)
	}
	var workspace struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(createWorkspace.Body).Decode(&workspace); err != nil {
		t.Fatal(err)
	}

	submit := ts.doJSON(t, "POST", "/api/v1/current-workspace/builds", token, `{"targetName":"app","imageTag":"v1"}`)
	defer submit.Body.Close()
	if submit.StatusCode != http.StatusCreated {
		t.Fatalf("submit current build status = %d, want 201", submit.StatusCode)
	}
	var job struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(submit.Body).Decode(&job); err != nil {
		t.Fatal(err)
	}

	deleteActive := ts.doJSON(t, "DELETE", "/api/v1/workspaces/"+workspace.ID, token, "")
	defer deleteActive.Body.Close()
	if deleteActive.StatusCode != http.StatusConflict {
		t.Fatalf("delete workspace with active job status = %d, want 409", deleteActive.StatusCode)
	}

	cancel := ts.doJSON(t, "POST", "/api/v1/jobs/"+job.ID+"/cancel", token, "")
	defer cancel.Body.Close()
	if cancel.StatusCode != http.StatusOK {
		t.Fatalf("cancel job status = %d, want 200", cancel.StatusCode)
	}

	deleteAfterCancel := ts.doJSON(t, "DELETE", "/api/v1/workspaces/"+workspace.ID, token, "")
	defer deleteAfterCancel.Body.Close()
	if deleteAfterCancel.StatusCode != http.StatusNoContent {
		t.Fatalf("delete workspace after cancel status = %d, want 204", deleteAfterCancel.StatusCode)
	}
}

func TestDeveloperWorkflowJobFailsWhenWorkflowDisappears(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)
	ts.createUserWithRole(t, "alice", testPassword, roles.DeveloperRoleID)
	token := ts.login(t, "alice", testPassword)
	fake := ts.fakeWorkflowEngine(t)
	fake.Behavior = func(spec workflows.Spec) workflows.Status { return workflows.Status{Phase: workflows.PhaseRunning} }

	createWorkspace := ts.doJSON(t, "POST", "/api/v1/workspaces", token, `{"name":"app","workProfile":"builder","repo":"app","sourceRef":"0123456789abcdef0123456789abcdef01234567"}`)
	defer createWorkspace.Body.Close()
	if createWorkspace.StatusCode != http.StatusCreated {
		t.Fatalf("create workspace status = %d, want 201", createWorkspace.StatusCode)
	}
	submit := ts.doJSON(t, "POST", "/api/v1/current-workspace/builds", token, `{"targetName":"app","imageTag":"v1"}`)
	defer submit.Body.Close()
	if submit.StatusCode != http.StatusCreated {
		t.Fatalf("submit current build status = %d, want 201", submit.StatusCode)
	}
	var job struct {
		ID      string `json:"id"`
		BuildID string `json:"buildId"`
	}
	if err := json.NewDecoder(submit.Body).Decode(&job); err != nil {
		t.Fatal(err)
	}

	fake.Delete("build-" + job.BuildID)
	status := ts.doJSON(t, "GET", "/api/v1/jobs/"+job.ID, token, "")
	defer status.Body.Close()
	if status.StatusCode != http.StatusOK {
		t.Fatalf("job status after missing workflow = %d, want 200", status.StatusCode)
	}
	var got struct {
		Status     string `json:"status"`
		ReasonCode string `json:"reasonCode"`
	}
	if err := json.NewDecoder(status.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "failed" || got.ReasonCode != "workflow_not_found" {
		t.Fatalf("job after missing workflow = %+v, want failed/workflow_not_found", got)
	}
}

func TestDeveloperWorkflowRoutesAbsentWhenBuildCapabilityDisabled(t *testing.T) {
	ts := newTestServerWithProfile(t, appliance.ProfileCore)
	ts.bootstrapAdmin(t, "admin", testPassword)
	token := ts.login(t, "admin", testPassword)

	for _, path := range []string{"/api/v1/work-profiles", "/api/v1/workspaces", "/api/v1/jobs"} {
		resp := ts.doJSON(t, "GET", path, token, "")
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want 404", path, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

func TestCurrentWorkspaceBuildStatusNotFoundBeforeBuild(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)
	ts.createUserWithRole(t, "alice", testPassword, roles.DeveloperRoleID)
	token := ts.login(t, "alice", testPassword)

	createWorkspace := ts.doJSON(t, "POST", "/api/v1/workspaces", token, `{"name":"app","workProfile":"builder","repo":"app","sourceRef":"0123456789abcdef0123456789abcdef01234567"}`)
	defer createWorkspace.Body.Close()
	if createWorkspace.StatusCode != http.StatusCreated {
		t.Fatalf("create workspace status = %d, want 201", createWorkspace.StatusCode)
	}

	resp := ts.doJSON(t, "GET", "/api/v1/current-workspace/build-status", token, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("current workspace build status before build = %d, want 404", resp.StatusCode)
	}
}

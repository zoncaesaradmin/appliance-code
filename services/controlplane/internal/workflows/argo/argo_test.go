package argo

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"appliance-code/services/controlplane/internal/workflows"
)

func TestSubmitCreatesStructuredWorkflow(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/apis/argoproj.io/v1alpha1/namespaces/appliance-builds/workflows" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-token" {
			t.Fatalf("Authorization = %q", auth)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"status":{"phase":"Running"}}`))
	}))
	defer server.Close()

	engine, err := New(Config{Namespace: "appliance-builds", InstanceID: "appliance", BaseURL: server.URL, BearerToken: "test-token", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	err = engine.Submit(t.Context(), workflows.Spec{Name: "build-1", SourceRepoURL: "git@git.internal.example.com:team/app.git", SourceCommitSHA: "0123456789abcdef0123456789abcdef01234567", ContainerfilePath: "Containerfile", BuilderImageDigest: "builder@sha256:abc", TargetRepository: "registry.local/users/alice/app", TargetTag: "v1", SourceCredentialSecret: "builder-git-key", KnownHostsSecret: "builder-git-known-hosts", Deadline: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if got["kind"] != "Workflow" {
		t.Fatalf("kind = %v, want Workflow", got["kind"])
	}
	body, _ := json.Marshal(got)
	text := string(body)
	for _, want := range []string{"builder-git-key", "builder-git-known-hosts", "GIT_SSH_COMMAND", "SOURCE_COMMIT_SHA", "buildah bud", "workflows.argoproj.io/controller-instanceid", "appliance"} {
		if !strings.Contains(text, want) {
			t.Fatalf("workflow JSON missing %q: %s", want, text)
		}
	}
}

func TestSubmitRejectsCredentialWithoutKnownHosts(t *testing.T) {
	_, err := workflowObject("appliance-builds", "", workflows.Spec{
		Name: "build-1", SourceRepoURL: "git@git.internal.example.com:team/app.git", SourceCommitSHA: "0123456789abcdef0123456789abcdef01234567",
		ContainerfilePath: "Containerfile", BuilderImageDigest: "builder@sha256:abc", TargetRepository: "registry.local/users/alice/app",
		TargetTag: "v1", SourceCredentialSecret: "builder-git-key", Deadline: time.Now().Add(time.Hour),
	})
	if err == nil {
		t.Fatal("workflowObject should reject builder Git secret usage without known_hosts secret")
	}
	if !strings.Contains(err.Error(), "known_hosts") {
		t.Fatalf("workflowObject error = %v, want known_hosts mentioned", err)
	}
}

func TestSubmitCreatesRepoScriptWorkflow(t *testing.T) {
	got, err := workflowObject("appliance-builds", "", workflows.Spec{
		Name: "build-1", SourceRepoURL: "https://git.internal.example.com/team/app", SourceCommitSHA: "0123456789abcdef0123456789abcdef01234567",
		Execution: "repo_script", ScriptPath: "scripts/build-image.sh", ContainerfilePath: "Containerfile",
		BuilderImageDigest: "builder@sha256:abc", TargetRepository: "registry.local/users/alice/app", TargetTag: "v1",
		Deadline: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("workflowObject: %v", err)
	}
	text := workflowJSON(t, got)
	command := workflowCommand(t, got)
	for _, want := range []string{"SCRIPT_PATH", "scripts/build-image.sh"} {
		if !strings.Contains(text, want) {
			t.Fatalf("repo_script workflow JSON missing %q: %s", want, text)
		}
	}
	for _, want := range []string{"chmod +x \"$SCRIPT_PATH\"", "\"./$SCRIPT_PATH\""} {
		if !strings.Contains(command, want) {
			t.Fatalf("repo_script command missing %q: %s", want, command)
		}
	}
	if strings.Contains(text, "buildah bud") {
		t.Fatalf("repo_script workflow should not use default buildah command: %s", text)
	}
}

func TestSubmitCreatesMakeTargetWorkflow(t *testing.T) {
	got, err := workflowObject("appliance-builds", "", workflows.Spec{
		Name: "build-1", SourceRepoURL: "https://git.internal.example.com/team/app", SourceCommitSHA: "0123456789abcdef0123456789abcdef01234567",
		Execution: "make_target", MakeTarget: "image", ContainerfilePath: "Containerfile",
		BuilderImageDigest: "builder@sha256:abc", TargetRepository: "registry.local/users/alice/app", TargetTag: "v1",
		Deadline: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("workflowObject: %v", err)
	}
	text := workflowJSON(t, got)
	command := workflowCommand(t, got)
	for _, want := range []string{"MAKE_TARGET"} {
		if !strings.Contains(text, want) {
			t.Fatalf("make_target workflow JSON missing %q: %s", want, text)
		}
	}
	for _, want := range []string{"make \"$MAKE_TARGET\"", "TARGET_IMAGE=\"$TARGET_IMAGE\""} {
		if !strings.Contains(command, want) {
			t.Fatalf("make_target command missing %q: %s", want, command)
		}
	}
	if strings.Contains(text, "buildah bud") {
		t.Fatalf("make_target workflow should not use default buildah command: %s", text)
	}
}

func TestSubmitCreatesBuildWorkflowWithSharedWorkspaceMount(t *testing.T) {
	got, err := workflowObject("appliance-builds", "", workflows.Spec{
		Name:               "build-1",
		BuilderImageDigest: "builder@sha256:abc",
		SourceRepoURL:      "https://git.internal.example.com/team/app",
		SourceCommitSHA:    "0123456789abcdef0123456789abcdef01234567",
		TargetRepository:   "registry.local/users/alice/app",
		TargetTag:          "v1",
		WorkspaceRootDir:   "/var/lib/zon/workspaces",
		WorkspaceClaimName: "control-plane-workspaces",
		Deadline:           time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("workflowObject: %v", err)
	}
	text := workflowJSON(t, got)
	for _, want := range []string{"workspace-storage", "control-plane-workspaces", "WORKSPACE_ROOT_DIR", "/var/lib/zon/workspaces"} {
		if !strings.Contains(text, want) {
			t.Fatalf("build workflow JSON missing %q: %s", want, text)
		}
	}
}

func TestSubmitCreatesWorkspacePrepareWorkflow(t *testing.T) {
	got, err := workflowObject("appliance-builds", "", workflows.Spec{
		Name:               "workspace-prepare-1",
		Kind:               workflows.KindWorkspacePrepare,
		BuilderImageDigest: "builder@sha256:abc",
		WorkspaceRootDir:   "/var/lib/zon/workspaces",
		WorkspaceClaimName: "control-plane-workspaces",
		WorkspaceName:      "demo",
		WorkspaceRepos: []workflows.WorkspaceRepo{
			{Name: "platformkit", URL: "git@git.internal.example.com:team/platformkit.git", Ref: "0123456789abcdef0123456789abcdef01234567"},
			{Name: "forgeline", URL: "https://git.internal.example.com/team/forgeline", Ref: "main"},
		},
		SourceCredentialSecret: "builder-git-key",
		KnownHostsSecret:       "builder-git-known-hosts",
		Deadline:               time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("workflowObject: %v", err)
	}
	text := workflowJSON(t, got)
	command := workflowCommand(t, got)
	for _, want := range []string{"workspace-storage", "control-plane-workspaces", "WORKSPACE_ROOT_DIR", "WORKSPACE_NAME", "platformkit", "forgeline"} {
		if !strings.Contains(text, want) {
			t.Fatalf("workspace workflow JSON missing %q: %s", want, text)
		}
	}
	for _, want := range []string{"git clone", "git -C 'platformkit' checkout", "git -C 'forgeline' checkout"} {
		if !strings.Contains(command, want) {
			t.Fatalf("workspace workflow command missing %q: %s", want, command)
		}
	}
}

func workflowJSON(t *testing.T, workflow map[string]any) string {
	t.Helper()
	body, err := json.Marshal(workflow)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func workflowCommand(t *testing.T, workflow map[string]any) string {
	t.Helper()
	spec := workflow["spec"].(map[string]any)
	templates := spec["templates"].([]map[string]any)
	container := templates[0]["container"].(map[string]any)
	args := container["args"].([]string)
	if len(args) != 1 {
		t.Fatalf("workflow args = %v, want one shell script", args)
	}
	return args[0]
}

func TestStatusCancelAndLogs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/apis/argoproj.io/v1alpha1/namespaces/appliance-builds/workflows/build-1":
			_, _ = w.Write([]byte(`{"status":{"phase":"Succeeded"}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/apis/argoproj.io/v1alpha1/namespaces/appliance-builds/workflows/build-1":
			if ct := r.Header.Get("Content-Type"); ct != "application/merge-patch+json" {
				t.Fatalf("patch content type = %q", ct)
			}
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/namespaces/appliance-builds/pods":
			if got := r.URL.Query().Get("labelSelector"); got != "workflows.argoproj.io/workflow=build-1" {
				t.Fatalf("labelSelector = %q", got)
			}
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"build-1-pod"}}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/namespaces/appliance-builds/pods/build-1-pod/log":
			if got := r.URL.Query().Get("container"); got != "main" {
				t.Fatalf("container = %q", got)
			}
			_, _ = w.Write([]byte("hello logs"))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()
	engine, err := New(Config{Namespace: "appliance-builds", BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	status, err := engine.Status(t.Context(), "build-1")
	if err != nil {
		t.Fatal(err)
	}
	if status.Phase != workflows.PhaseSucceeded {
		t.Fatalf("phase = %q", status.Phase)
	}
	if err := engine.Cancel(t.Context(), "build-1"); err != nil {
		t.Fatal(err)
	}
	logs, err := engine.Logs(t.Context(), "build-1")
	if err != nil {
		t.Fatal(err)
	}
	if logs != "hello logs" {
		t.Fatalf("logs = %q", logs)
	}
}

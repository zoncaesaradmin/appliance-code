package devflows

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/logging"
	"appliance-code/services/controlplane/internal/storage"
	"appliance-code/services/controlplane/internal/storage/sqlite"
	"appliance-code/services/controlplane/internal/workflows"
	"github.com/zoncaesaradmin/platformkit/ctxutil"
)

func TestNewServiceRequiresLogger(t *testing.T) {
	_, err := NewService(Catalog{}, nil, nil, nil, nil, "", "", nil, nil, nil)
	if err == nil {
		t.Fatal("NewService should reject a nil logger")
	}
}

func TestWorkspaceProvisioningLogsSubmissionAndStatus(t *testing.T) {
	ctx := ctxutil.WithTraceID(context.Background(), "trace-workspace-provision-123")
	db := openDevflowsTestDB(t)
	defer db.Close()

	var logBuf bytes.Buffer
	logger, err := logging.NewWithWriter("info", &logBuf)
	if err != nil {
		t.Fatalf("NewWithWriter: %v", err)
	}

	engine := workflows.NewFake()
	engine.Behavior = func(spec workflows.Spec) workflows.Status {
		return workflows.Status{Phase: workflows.PhasePending}
	}
	now := time.Now().UTC()
	if err := sqlite.NewUserStore(db).Create(ctx, storage.User{
		ID:          "user-1",
		Username:    "alice",
		DisplayName: "Alice",
		State:       storage.UserStateActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("Create user: %v", err)
	}

	svc, err := NewService(testProvisionCatalog(), sqlite.NewWorkspaceStore(db), sqlite.NewJobStore(db), nil, engine, "/data/zon/workspaces", "appliance-workspaces", nil, logger, nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	ws, err := svc.CreateWorkspace(ctx, audit.Actor{Type: storage.AuditActorUser}, "user-1", CreateWorkspaceRequest{Name: "myws1", WorkProfile: "simulation-dev"})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}

	submitted := findLogRecord(t, logBuf.String(), "workspace provisioning workflow submitted")
	if got := submitted["traceId"]; got != "trace-workspace-provision-123" {
		t.Fatalf("submitted traceId = %#v, want trace-workspace-provision-123", got)
	}
	if got := submitted["workspaceName"]; got != "myws1" {
		t.Fatalf("submitted workspaceName = %#v, want myws1", got)
	}
	if got := submitted["workflowName"]; got == "" {
		t.Fatalf("submitted workflowName = %#v, want non-empty", got)
	}
	if got := submitted["jobID"]; got == "" {
		t.Fatalf("submitted jobID = %#v, want non-empty", got)
	}

	job, err := svc.latestWorkspacePrepareJob(ctx, ws.ID)
	if err != nil {
		t.Fatalf("latestWorkspacePrepareJob: %v", err)
	}
	workflowName := workspacePrepareWorkflowName(job.ID)
	engine.SetStatus(workflowName, workflows.Status{Phase: workflows.PhaseSucceeded})

	reconciled, err := svc.GetWorkspace(ctx, ws.ID, "user-1", false)
	if err != nil {
		t.Fatalf("GetWorkspace: %v", err)
	}
	if reconciled.Status != storage.WorkspaceStatusReady {
		t.Fatalf("workspace status = %q, want ready", reconciled.Status)
	}

	stateChanged := findLogRecord(t, logBuf.String(), "workspace provisioning workflow state changed")
	if got := stateChanged["traceId"]; got != "trace-workspace-provision-123" {
		t.Fatalf("stateChanged traceId = %#v, want trace-workspace-provision-123", got)
	}
	if got := stateChanged["workflowPhase"]; got != "succeeded" {
		t.Fatalf("workflowPhase = %#v, want succeeded", got)
	}
	if got := stateChanged["jobStatus"]; got != "succeeded" {
		t.Fatalf("jobStatus = %#v, want succeeded", got)
	}

	statusReconciled := findLogRecord(t, logBuf.String(), "workspace status reconciled")
	if got := statusReconciled["status"]; got != "ready" {
		t.Fatalf("status = %#v, want ready", got)
	}
	if got := statusReconciled["previousStatus"]; got != "pending" {
		t.Fatalf("previousStatus = %#v, want pending", got)
	}
}

func openDevflowsTestDB(t *testing.T) *sqlite.DB {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "devflows.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	return db
}

func testProvisionCatalog() Catalog {
	return Catalog{
		WorkProfiles: []WorkProfile{{
			Name: "simulation-dev",
			Repos: []ProfileRepo{
				{Name: "platformkit", EnabledByDefault: true},
				{Name: "forgeline", EnabledByDefault: true},
			},
		}},
		Repos: []Repo{
			{Name: "platformkit", URL: "https://git.internal.example.com/team/platformkit.git", DefaultRef: "main"},
			{Name: "forgeline", URL: "https://git.internal.example.com/team/forgeline.git", DefaultRef: "main"},
		},
		WorkspaceProvisionerImageDigest: "workspace-provisioner@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		BuildTargets: []BuildTarget{{
			Name:               "default",
			Repo:               "forgeline",
			Execution:          ExecutionRepoScript,
			ContainerfilePath:  "Containerfile",
			ImageRepository:    "users/test/app",
			BuilderImageDigest: "buildah@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}},
	}
}

func findLogRecord(t *testing.T, text, message string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("parse log JSON: %v\nlog=%s", err, line)
		}
		if record["message"] == message || record["msg"] == message {
			return record
		}
	}
	t.Fatalf("did not find log message %q in %s", message, text)
	return nil
}

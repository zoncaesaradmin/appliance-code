package builds_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/builds"
	"appliance-code/services/controlplane/internal/storage"
	"appliance-code/services/controlplane/internal/storage/sqlite"
	"appliance-code/services/controlplane/internal/workflows"
)

const validCommitSHA = "0123456789abcdef0123456789abcdef01234567"

func systemActor() audit.Actor {
	return audit.Actor{Type: storage.AuditActorSystem, AuthMethod: "test"}
}

type harness struct {
	db   *sqlite.DB
	svc  *builds.Service
	fake *workflows.Fake
}

type failingSubmitEngine struct {
	err error
}

func (e failingSubmitEngine) Submit(context.Context, workflows.Spec) error {
	return e.err
}

func (e failingSubmitEngine) Status(context.Context, string) (workflows.Status, error) {
	return workflows.Status{}, workflows.ErrNotFound
}

func (e failingSubmitEngine) Cancel(context.Context, string) error {
	return nil
}

func (e failingSubmitEngine) Logs(context.Context, string) (string, error) {
	return "", nil
}

type leakingLogEngine struct {
	workflowName string
}

func (e *leakingLogEngine) Submit(_ context.Context, spec workflows.Spec) error {
	e.workflowName = spec.Name
	return nil
}

func (e *leakingLogEngine) Status(context.Context, string) (workflows.Status, error) {
	return workflows.Status{Phase: workflows.PhaseRunning}, nil
}

func (e *leakingLogEngine) Cancel(context.Context, string) error {
	return nil
}

func (e *leakingLogEngine) Logs(context.Context, string) (string, error) {
	return "using source-secret-a and source-secret-b\n-----BEGIN OPENSSH PRIVATE KEY-----\nsecret\n-----END OPENSSH PRIVATE KEY-----\n", nil
}

func newHarness(t *testing.T, deadline time.Duration) *harness {
	t.Helper()
	ctx := context.Background()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "appliance.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	auditStore := sqlite.NewAuditStore(db)
	recorder := audit.NewRecorder(auditStore)
	buildStore := sqlite.NewBuildStore(db)
	idempotencyStore := sqlite.NewIdempotencyStore(db)
	fake := workflows.NewFake()

	svc := builds.NewService(db, buildStore, idempotencyStore, fake, recorder,
		[]string{"git.internal.example.com"}, []string{"buildah@sha256:approved"}, deadline,
		"/data/zon/workspaces", "control-plane-workspaces", nil)
	return &harness{db: db, svc: svc, fake: fake}
}

func validRequest() builds.CreateRequest {
	return builds.CreateRequest{
		SourceRepoURL: "https://git.internal.example.com/team/app", SourceCommitSHA: validCommitSHA,
		ImageRepository: "users/alice/app", ImageTag: "v1", BuilderImageDigest: "buildah@sha256:approved",
	}
}

func TestCreateSucceedsByDefault(t *testing.T) {
	h := newHarness(t, time.Hour)
	build, err := h.svc.Create(t.Context(), systemActor(), "user-1", validRequest(), "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if build.Status != storage.BuildStatusRunning {
		t.Errorf("status after submit = %q, want running", build.Status)
	}
	if build.ContainerfilePath != "Containerfile" {
		t.Errorf("default containerfile path = %q, want Containerfile", build.ContainerfilePath)
	}

	h.fake.SetStatus(build.WorkflowName, workflows.Status{Phase: workflows.PhaseSucceeded})
	got, err := h.svc.Get(t.Context(), build.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != storage.BuildStatusSucceeded {
		t.Errorf("status after reconcile = %q, want succeeded", got.Status)
	}
	if got.CompletedAt == nil {
		t.Error("CompletedAt should be set once succeeded")
	}
}

func TestCreateReflectsWorkflowFailure(t *testing.T) {
	h := newHarness(t, time.Hour)
	h.fake.Behavior = func(spec workflows.Spec) workflows.Status { return workflows.Status{Phase: workflows.PhaseRunning} }

	build, err := h.svc.Create(t.Context(), systemActor(), "user-1", validRequest(), "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	h.fake.SetStatus(build.WorkflowName, workflows.Status{Phase: workflows.PhaseFailed, Message: "compile error"})

	got, err := h.svc.Get(t.Context(), build.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != storage.BuildStatusFailed || got.ErrorMessage != "compile error" {
		t.Errorf("got status=%q errorMessage=%q, want failed/compile error", got.Status, got.ErrorMessage)
	}
}

func TestWorkflowFailureMessageRedactsSensitiveValues(t *testing.T) {
	h := newHarness(t, time.Hour)
	svc := builds.NewService(h.db, sqlite.NewBuildStore(h.db), sqlite.NewIdempotencyStore(h.db), h.fake, audit.NewRecorder(sqlite.NewAuditStore(h.db)),
		[]string{"git.internal.example.com"}, []string{"buildah@sha256:approved"}, time.Hour,
		"/data/zon/workspaces", "control-plane-workspaces", nil, "source-secret-a", "source-secret-b")
	h.fake.Behavior = func(spec workflows.Spec) workflows.Status {
		return workflows.Status{Phase: workflows.PhaseRunning}
	}

	build, err := svc.Create(t.Context(), systemActor(), "user-1", validRequest(), "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	h.fake.SetStatus(build.WorkflowName, workflows.Status{Phase: workflows.PhaseFailed, Message: "clone failed using source-secret-a and source-secret-b"})

	got, err := svc.Get(t.Context(), build.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if strings.Contains(got.ErrorMessage, "source-secret-a") || strings.Contains(got.ErrorMessage, "source-secret-b") {
		t.Fatalf("error message leaked source credential secret names: %q", got.ErrorMessage)
	}
	if got.ErrorMessage != "clone failed using [REDACTED] and [REDACTED]" {
		t.Fatalf("error message = %q, want redacted secret names", got.ErrorMessage)
	}
}

func TestLogsRedactSensitiveValues(t *testing.T) {
	h := newHarness(t, time.Hour)
	engine := &leakingLogEngine{}
	svc := builds.NewService(h.db, sqlite.NewBuildStore(h.db), sqlite.NewIdempotencyStore(h.db), engine, audit.NewRecorder(sqlite.NewAuditStore(h.db)),
		[]string{"git.internal.example.com"}, []string{"buildah@sha256:approved"}, time.Hour,
		"/data/zon/workspaces", "control-plane-workspaces", nil, "source-secret-a", "source-secret-b")

	build, err := svc.Create(t.Context(), systemActor(), "user-1", validRequest(), "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	logs, err := svc.Logs(t.Context(), build.ID)
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	for _, secret := range []string{"source-secret-a", "source-secret-b", "OPENSSH PRIVATE KEY", "secret"} {
		if strings.Contains(logs, secret) {
			t.Fatalf("logs leaked %q: %s", secret, logs)
		}
	}
	if strings.Count(logs, "[REDACTED]") < 3 {
		t.Fatalf("logs = %q, want redaction markers", logs)
	}
}

func TestCreateRecordsFailedBuildWhenWorkflowSubmitFails(t *testing.T) {
	h := newHarness(t, time.Hour)
	submitErr := errors.New("kubernetes API unavailable")
	svc := builds.NewService(h.db, sqlite.NewBuildStore(h.db), sqlite.NewIdempotencyStore(h.db), failingSubmitEngine{err: submitErr}, audit.NewRecorder(sqlite.NewAuditStore(h.db)),
		[]string{"git.internal.example.com"}, []string{"buildah@sha256:approved"}, time.Hour,
		"/data/zon/workspaces", "control-plane-workspaces", nil)

	build, err := svc.Create(t.Context(), systemActor(), "user-1", validRequest(), "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if build.Status != storage.BuildStatusFailed || build.ReasonCode != "workflow_submit_failed" {
		t.Fatalf("build after submit failure = status %q reason %q, want failed/workflow_submit_failed", build.Status, build.ReasonCode)
	}
	if build.ErrorMessage != submitErr.Error() {
		t.Fatalf("error message = %q, want %q", build.ErrorMessage, submitErr.Error())
	}
	if build.CompletedAt == nil {
		t.Fatal("failed build should record CompletedAt")
	}

	persisted, err := sqlite.NewBuildStore(h.db).Get(t.Context(), build.ID)
	if err != nil {
		t.Fatalf("Get persisted build: %v", err)
	}
	if persisted.Status != storage.BuildStatusFailed || persisted.ReasonCode != "workflow_submit_failed" || persisted.ErrorMessage != submitErr.Error() {
		t.Fatalf("persisted build = %+v, want durable submit failure", persisted)
	}
}

func TestBuildTimesOutPastDeadline(t *testing.T) {
	h := newHarness(t, -time.Second) // deadline already in the past
	h.fake.Behavior = func(spec workflows.Spec) workflows.Status { return workflows.Status{Phase: workflows.PhaseRunning} }

	build, err := h.svc.Create(t.Context(), systemActor(), "user-1", validRequest(), "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := h.svc.Get(t.Context(), build.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != storage.BuildStatusTimedOut {
		t.Errorf("status = %q, want timed_out", got.Status)
	}
	if !h.fake.WasCancelled(build.WorkflowName) {
		t.Error("timed-out build should cancel its underlying workflow")
	}
}

func TestWorkflowDisappearsTransitionsBuildToFailed(t *testing.T) {
	h := newHarness(t, time.Hour)
	h.fake.Behavior = func(spec workflows.Spec) workflows.Status { return workflows.Status{Phase: workflows.PhaseRunning} }

	build, err := h.svc.Create(t.Context(), systemActor(), "user-1", validRequest(), "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	missingSvc := builds.NewService(h.db, sqlite.NewBuildStore(h.db), sqlite.NewIdempotencyStore(h.db), workflows.NewFake(), audit.NewRecorder(sqlite.NewAuditStore(h.db)),
		[]string{"git.internal.example.com"}, []string{"buildah@sha256:approved"}, time.Hour,
		"/data/zon/workspaces", "control-plane-workspaces", nil)

	got, err := missingSvc.Get(t.Context(), build.ID)
	if err != nil {
		t.Fatalf("Get with missing workflow: %v", err)
	}
	if got.Status != storage.BuildStatusFailed || got.ReasonCode != "workflow_not_found" {
		t.Fatalf("missing workflow build = status %q reason %q, want failed/workflow_not_found", got.Status, got.ReasonCode)
	}
	if got.CompletedAt == nil {
		t.Fatal("missing workflow build should record CompletedAt")
	}
}

func TestCancelTransitionsToCancelled(t *testing.T) {
	h := newHarness(t, time.Hour)
	h.fake.Behavior = func(spec workflows.Spec) workflows.Status { return workflows.Status{Phase: workflows.PhaseRunning} }

	build, err := h.svc.Create(t.Context(), systemActor(), "user-1", validRequest(), "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := h.svc.Cancel(t.Context(), systemActor(), build.ID)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	// The fake's Cancel immediately marks the underlying workflow Failed,
	// which reconcile() maps to Cancelled because CancelRequested is set.
	if got.Status != storage.BuildStatusCancelled {
		t.Errorf("status after cancel = %q, want cancelled", got.Status)
	}
}

func TestCreateIsIdempotent(t *testing.T) {
	h := newHarness(t, time.Hour)
	first, err := h.svc.Create(t.Context(), systemActor(), "user-1", validRequest(), "key-1")
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}
	second, err := h.svc.Create(t.Context(), systemActor(), "user-1", validRequest(), "key-1")
	if err != nil {
		t.Fatalf("second Create: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("second Create with the same idempotency key returned a different build: %s vs %s", second.ID, first.ID)
	}

	// A different request body under the same key must be rejected rather
	// than silently returning the first build.
	other := validRequest()
	other.ImageTag = "v2"
	if _, err := h.svc.Create(t.Context(), systemActor(), "user-1", other, "key-1"); !errors.Is(err, builds.ErrIdempotencyKeyReused) {
		t.Errorf("reused key with different body error = %v, want ErrIdempotencyKeyReused", err)
	}
}

func TestCreateRejectsUnlistedGitHost(t *testing.T) {
	h := newHarness(t, time.Hour)
	req := validRequest()
	req.SourceRepoURL = "https://evil.example.com/team/app"
	if _, err := h.svc.Create(t.Context(), systemActor(), "user-1", req, ""); err == nil {
		t.Error("Create should reject a source host outside the allowlist")
	}
}

func TestCreateRejectsUnsupportedSourceScheme(t *testing.T) {
	h := newHarness(t, time.Hour)
	req := validRequest()
	req.SourceRepoURL = "http://git.internal.example.com/team/app"
	if _, err := h.svc.Create(t.Context(), systemActor(), "user-1", req, ""); err == nil {
		t.Error("Create should reject unsupported source URL schemes")
	}
}

func TestCreateRejectsSSHSource(t *testing.T) {
	h := newHarness(t, time.Hour)
	req := validRequest()
	req.SourceRepoURL = "git@git.internal.example.com:team/app.git"
	if _, err := h.svc.Create(t.Context(), systemActor(), "user-1", req, ""); err == nil {
		t.Error("Create should reject SSH source URLs")
	}
}

func TestCreateRejectsSSHCredentialInputsForHTTPSSource(t *testing.T) {
	h := newHarness(t, time.Hour)
	req := validRequest()
	req.SourceCredentialSecret = "source-secret-a"
	req.KnownHostsSecret = "source-secret-b"
	if _, err := h.svc.Create(t.Context(), systemActor(), "user-1", req, ""); err == nil {
		t.Fatal("Create should reject SSH credential inputs for HTTPS sources")
	}
}

func TestCreateRejectsMalformedCommitSHA(t *testing.T) {
	h := newHarness(t, time.Hour)
	req := validRequest()
	req.SourceCommitSHA = "not-a-commit-sha"
	if _, err := h.svc.Create(t.Context(), systemActor(), "user-1", req, ""); err == nil {
		t.Error("Create should reject a malformed commit SHA")
	}
}

func TestCreateRejectsUnapprovedBuilderImage(t *testing.T) {
	h := newHarness(t, time.Hour)
	req := validRequest()
	req.BuilderImageDigest = "buildah@sha256:not-approved"
	if _, err := h.svc.Create(t.Context(), systemActor(), "user-1", req, ""); err == nil {
		t.Error("Create should reject a builder image outside the allowlist")
	}
}

func TestCreateMountsSharedWorkspaceStorageIntoWorkflow(t *testing.T) {
	h := newHarness(t, time.Hour)
	build, err := h.svc.Create(t.Context(), systemActor(), "user-1", validRequest(), "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	spec, ok := h.fake.SubmittedSpec(build.WorkflowName)
	if !ok {
		t.Fatalf("workflow spec %q was not submitted", build.WorkflowName)
	}
	if spec.WorkspaceRootDir != "/data/zon/workspaces" || spec.WorkspaceClaimName != "control-plane-workspaces" {
		t.Fatalf("workflow workspace mount fields = %+v, want shared workspace storage mounted", spec)
	}
}

func TestCreatePassesStructuredExecutionToWorkflow(t *testing.T) {
	h := newHarness(t, time.Hour)
	req := validRequest()
	req.Execution = "make_target"
	req.MakeTarget = "image"
	req.ContainerfilePath = "deploy/Containerfile"
	build, err := h.svc.Create(t.Context(), systemActor(), "user-1", req, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	spec, ok := h.fake.SubmittedSpec(build.WorkflowName)
	if !ok {
		t.Fatalf("workflow spec %q was not submitted", build.WorkflowName)
	}
	if spec.Execution != "make_target" || spec.MakeTarget != "image" || spec.ContainerfilePath != "deploy/Containerfile" {
		t.Fatalf("workflow spec execution fields = %+v", spec)
	}
}

func TestListFiltersByOwner(t *testing.T) {
	h := newHarness(t, time.Hour)
	if _, err := h.svc.Create(t.Context(), systemActor(), "user-1", validRequest(), ""); err != nil {
		t.Fatalf("Create for user-1: %v", err)
	}
	if _, err := h.svc.Create(t.Context(), systemActor(), "user-2", validRequest(), ""); err != nil {
		t.Fatalf("Create for user-2: %v", err)
	}

	list, err := h.svc.List(t.Context(), storage.BuildFilter{OwnerID: "user-1"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].OwnerID != "user-1" {
		t.Errorf("filtered list = %+v, want exactly one build owned by user-1", list)
	}
}

// TestRestartRecovery simulates a control-plane restart: a fresh Service
// backed by the same database and a fresh (but state-preserving) engine
// handle must still be able to reconcile an in-flight build to completion.
func TestRestartRecovery(t *testing.T) {
	h := newHarness(t, time.Hour)
	h.fake.Behavior = func(spec workflows.Spec) workflows.Status { return workflows.Status{Phase: workflows.PhaseRunning} }

	build, err := h.svc.Create(t.Context(), systemActor(), "user-1", validRequest(), "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	h.fake.SetStatus(build.WorkflowName, workflows.Status{Phase: workflows.PhaseSucceeded})

	// Rebuild the service (simulating a fresh process) against the same DB
	// and the same underlying engine (simulating Argo state surviving a
	// control-plane restart, which is the plan's stated model: Argo holds
	// operational state, SQLite holds durable state).
	recorder := audit.NewRecorder(sqlite.NewAuditStore(h.db))
	freshSvc := builds.NewService(h.db, sqlite.NewBuildStore(h.db), sqlite.NewIdempotencyStore(h.db), h.fake, recorder,
		[]string{"git.internal.example.com"}, []string{"buildah@sha256:approved"}, time.Hour,
		"/data/zon/workspaces", "control-plane-workspaces", nil)

	if err := freshSvc.ReconcileAll(t.Context()); err != nil {
		t.Fatalf("ReconcileAll: %v", err)
	}
	got, err := freshSvc.Get(t.Context(), build.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != storage.BuildStatusSucceeded {
		t.Errorf("status after restart recovery = %q, want succeeded", got.Status)
	}
}

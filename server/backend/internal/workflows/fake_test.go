package workflows_test

import (
	"errors"
	"testing"
	"time"

	"appliance-code/server/backend/internal/workflows"
)

func TestFakeSubmitDefaultsToSucceeded(t *testing.T) {
	fake := workflows.NewFake()
	spec := workflows.Spec{Name: "build-1", SourceRepoURL: "https://git.internal/x", SourceCommitSHA: "abc123", Deadline: time.Now().Add(time.Hour)}
	if err := fake.Submit(t.Context(), spec); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	status, err := fake.Status(t.Context(), "build-1")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.Phase != workflows.PhaseSucceeded {
		t.Errorf("phase = %q, want succeeded", status.Phase)
	}
}

func TestFakeRejectsDuplicateSubmission(t *testing.T) {
	fake := workflows.NewFake()
	spec := workflows.Spec{Name: "build-1"}
	if err := fake.Submit(t.Context(), spec); err != nil {
		t.Fatalf("first Submit: %v", err)
	}
	if err := fake.Submit(t.Context(), spec); err == nil {
		t.Error("second Submit with the same name should fail")
	}
}

func TestFakeStatusUnknownWorkflow(t *testing.T) {
	fake := workflows.NewFake()
	if _, err := fake.Status(t.Context(), "unknown"); !errors.Is(err, workflows.ErrNotFound) {
		t.Errorf("Status(unknown) error = %v, want ErrNotFound", err)
	}
}

func TestFakeBehaviorOverride(t *testing.T) {
	fake := workflows.NewFake()
	fake.Behavior = func(spec workflows.Spec) workflows.Status {
		return workflows.Status{Phase: workflows.PhaseFailed, Message: "simulated build failure"}
	}
	if err := fake.Submit(t.Context(), workflows.Spec{Name: "build-1"}); err != nil {
		t.Fatal(err)
	}
	status, err := fake.Status(t.Context(), "build-1")
	if err != nil {
		t.Fatal(err)
	}
	if status.Phase != workflows.PhaseFailed {
		t.Errorf("phase = %q, want failed", status.Phase)
	}
}

func TestFakeCancel(t *testing.T) {
	fake := workflows.NewFake()
	fake.Behavior = func(spec workflows.Spec) workflows.Status { return workflows.Status{Phase: workflows.PhaseRunning} }
	if err := fake.Submit(t.Context(), workflows.Spec{Name: "build-1"}); err != nil {
		t.Fatal(err)
	}
	if err := fake.Cancel(t.Context(), "build-1"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if !fake.WasCancelled("build-1") {
		t.Error("WasCancelled should report true after Cancel")
	}
	status, err := fake.Status(t.Context(), "build-1")
	if err != nil {
		t.Fatal(err)
	}
	if status.Phase != workflows.PhaseFailed {
		t.Errorf("phase after cancel = %q, want failed", status.Phase)
	}
}

func TestFakeSetStatusDrivesProgress(t *testing.T) {
	fake := workflows.NewFake()
	fake.Behavior = func(spec workflows.Spec) workflows.Status { return workflows.Status{Phase: workflows.PhaseRunning} }
	if err := fake.Submit(t.Context(), workflows.Spec{Name: "build-1"}); err != nil {
		t.Fatal(err)
	}
	fake.SetStatus("build-1", workflows.Status{Phase: workflows.PhaseSucceeded})

	status, err := fake.Status(t.Context(), "build-1")
	if err != nil {
		t.Fatal(err)
	}
	if status.Phase != workflows.PhaseSucceeded {
		t.Errorf("phase after SetStatus = %q, want succeeded", status.Phase)
	}
}

func TestFakeLogs(t *testing.T) {
	fake := workflows.NewFake()
	if err := fake.Submit(t.Context(), workflows.Spec{Name: "build-1", SourceRepoURL: "https://git.internal/x", SourceCommitSHA: "abc"}); err != nil {
		t.Fatal(err)
	}
	logs, err := fake.Logs(t.Context(), "build-1")
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if logs == "" {
		t.Error("Logs should return non-empty output for a known workflow")
	}
}

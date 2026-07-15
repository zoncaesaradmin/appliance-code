package workflows

import (
	"context"
	"fmt"
	"sync"
)

// Fake is an in-process Engine used by every test in this codebase, per
// the plan's local-first testing rule. By default Submit immediately marks
// a workflow Succeeded; set Behavior to control outcomes (failure, staying
// Running for a test to drive forward with SetStatus, and so on).
type Fake struct {
	mu sync.Mutex

	// Behavior computes the initial status for a newly submitted spec. If
	// nil, every submission immediately succeeds.
	Behavior func(spec Spec) Status

	workflows map[string]*fakeWorkflow
}

type fakeWorkflow struct {
	spec      Spec
	status    Status
	cancelled bool
}

// NewFake returns an empty Fake ready for tests to use and, optionally,
// configure via Behavior.
func NewFake() *Fake {
	return &Fake{workflows: map[string]*fakeWorkflow{}}
}

func (f *Fake) Submit(_ context.Context, spec Spec) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, exists := f.workflows[spec.Name]; exists {
		return fmt.Errorf("workflows: %s already submitted", spec.Name)
	}

	status := Status{Phase: PhaseSucceeded}
	if f.Behavior != nil {
		status = f.Behavior(spec)
	}
	f.workflows[spec.Name] = &fakeWorkflow{spec: spec, status: status}
	return nil
}

func (f *Fake) Status(_ context.Context, name string) (Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	wf, ok := f.workflows[name]
	if !ok {
		return Status{}, ErrNotFound
	}
	return wf.status, nil
}

func (f *Fake) Cancel(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	wf, ok := f.workflows[name]
	if !ok {
		return ErrNotFound
	}
	wf.cancelled = true
	wf.status = Status{Phase: PhaseFailed, Message: "cancelled"}
	return nil
}

func (f *Fake) Logs(_ context.Context, name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	wf, ok := f.workflows[name]
	if !ok {
		return "", ErrNotFound
	}
	return fmt.Sprintf("fake logs for workflow %s (source %s@%s)", wf.spec.Name, wf.spec.SourceRepoURL, wf.spec.SourceCommitSHA), nil
}

// SetStatus lets a test directly drive a previously submitted workflow's
// status forward, simulating asynchronous progress a reconciliation poll
// would observe (e.g. Pending -> Running -> Succeeded/Failed).
func (f *Fake) SetStatus(name string, status Status) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if wf, ok := f.workflows[name]; ok {
		wf.status = status
	}
}

// Delete removes a workflow from the fake engine, simulating an external
// deletion of the workflow resource before the control plane reconciles it.
func (f *Fake) Delete(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.workflows, name)
}

// WasCancelled reports whether Cancel was called for name, for tests that
// need to assert cancellation actually reached the engine.
func (f *Fake) WasCancelled(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	wf, ok := f.workflows[name]
	return ok && wf.cancelled
}

// SubmittedSpec returns the structured workflow spec captured at Submit time.
// It is intentionally test-oriented: production code should observe workflows
// through Status/Logs/Cancel, not by peeking into engine internals.
func (f *Fake) SubmittedSpec(name string) (Spec, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	wf, ok := f.workflows[name]
	if !ok {
		return Spec{}, false
	}
	return wf.spec, true
}

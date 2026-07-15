// Package workflows defines a workflow-engine contract in domain terms: the
// build service submits, polls, cancels, and reads logs for one build
// through this interface without knowing whether the real implementation
// is Argo Workflows or something else.
//
// The production implementation, internal/workflows/argo, submits and
// reconciles appliance-owned Argo Workflow resources through the Kubernetes
// API. Local unit and HTTP contract tests can still use the in-process Fake,
// matching the plan's local-first testing rule for cluster-facing interfaces.
package workflows

import (
	"context"
	"errors"
	"time"
)

// Phase is a workflow's coarse execution state.
type Phase string

const (
	PhasePending   Phase = "pending"
	PhaseRunning   Phase = "running"
	PhaseSucceeded Phase = "succeeded"
	PhaseFailed    Phase = "failed"
)

// ErrNotFound is returned when a named workflow is unknown to the engine.
var ErrNotFound = errors.New("workflows: workflow not found")

// Spec describes one build to run as an isolated workflow. It carries only
// structured values; nothing here is a free-form command or shell string,
// per the plan's "domain interfaces accept structured values" rule.
type Spec struct {
	Name                   string // caller-assigned unique workflow name, e.g. "build-<uuid>"
	SourceRepoURL          string
	SourceCommitSHA        string
	Execution              string
	ScriptPath             string
	MakeTarget             string
	ContainerfilePath      string
	BuilderImageDigest     string
	TargetRepository       string
	TargetTag              string
	SourceCredentialRef    string
	SourceCredentialSecret string
	KnownHostsSecret       string
	Deadline               time.Time
}

// Status is a workflow's last-observed state.
type Status struct {
	Phase   Phase
	Message string
}

// Engine is the domain-level workflow contract the build service depends
// on. It does not own build authorization or durable build state; the
// caller (internal/builds) owns reconciling Status into storage.Build.
type Engine interface {
	// Submit starts spec as a new workflow. Submitting the same Name twice
	// is an error; workflow names are meant to be unique per build.
	Submit(ctx context.Context, spec Spec) error

	// Status returns the named workflow's last-observed state.
	Status(ctx context.Context, name string) (Status, error)

	// Cancel requests termination of the named workflow.
	Cancel(ctx context.Context, name string) error

	// Logs returns the named workflow's available log output.
	Logs(ctx context.Context, name string) (string, error)
}

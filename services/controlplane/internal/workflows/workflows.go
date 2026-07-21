// Package workflows defines a workflow-engine contract in domain terms: the
// control plane submits, polls, cancels, and reads logs for one appliance-owned
// workflow without knowing whether the real implementation is Argo Workflows or
// something else.
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

// Kind identifies the appliance-owned workflow purpose.
type Kind string

const (
	KindBuild            Kind = "build"
	KindWorkspacePrepare Kind = "workspace_prepare"
)

// ErrNotFound is returned when a named workflow is unknown to the engine.
var ErrNotFound = errors.New("workflows: workflow not found")

// WorkspaceRepo is one repo that should be materialized into a workspace.
type WorkspaceRepo struct {
	Name string
	URL  string
	Ref  string
}

// Spec describes one workflow to run as an isolated workflow pod. It carries
// only structured values; nothing here is a free-form command or shell string.
type Spec struct {
	Name                   string
	Kind                   Kind
	BuilderImageDigest     string
	GitCredentialSecret    string
	SourceCredentialRef    string
	SourceCredentialSecret string
	KnownHostsSecret       string
	Deadline               time.Time

	SourceRepoURL     string
	SourceCommitSHA   string
	Execution         string
	Args              []string
	ContainerfilePath string
	TargetRepository  string
	TargetTag         string

	WorkspaceRootDir   string
	WorkspaceClaimName string
	WorkspaceName      string
	WorkspaceRepo      string
	WorkspaceRepos     []WorkspaceRepo
}

// Status is a workflow's last-observed state.
type Status struct {
	Phase   Phase
	Message string
}

// Engine is the domain-level workflow contract the control plane depends on.
type Engine interface {
	Submit(ctx context.Context, spec Spec) error
	Status(ctx context.Context, name string) (Status, error)
	Cancel(ctx context.Context, name string) error
	Logs(ctx context.Context, name string) (string, error)
}

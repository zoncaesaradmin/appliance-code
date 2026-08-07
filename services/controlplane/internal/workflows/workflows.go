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

// GitCredentialRef identifies one mounted builder Git HTTPS credential.
type GitCredentialRef struct {
	Name       string
	Host       string
	SecretName string
}

// Spec describes one workflow to run as an isolated workflow pod. It carries
// only structured values; nothing here is a free-form command or shell string.
type Spec struct {
	Name                   string
	Kind                   Kind
	BuilderImageDigest     string
	GitCredentials         []GitCredentialRef
	SourceCredentialRef    string
	SourceCredentialSecret string
	KnownHostsSecret       string
	Deadline               time.Time

	SourceRepoURL     string
	SourceCommitSHA   string
	Execution         string
	Args              []string
	WorkingDirectory  string
	ContainerfilePath string
	TargetRepository  string
	TargetTag         string

	// RegistryHost is the public artifact-server registry host (no scheme),
	// used as DEV_REGISTRY / SERVICE_IMAGE_REGISTRY and as the TARGET_IMAGE
	// registry prefix. Empty means the workflow does not push to a registry.
	RegistryHost string
	// RegistryTLSVerify is "true" or "false" for buildah/make DEV_REGISTRY_TLS_VERIFY.
	RegistryTLSVerify string
	// RegistryCredentialSecret is the Kubernetes Secret in the workflow
	// namespace that holds username and token keys for registry login.
	RegistryCredentialSecret string

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

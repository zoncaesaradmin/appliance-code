// Package zotadapter owns zot API calls, health, and error translation for
// the OCI image/artifact catalog. zot is authoritative for manifests, tags,
// digests, referrers, and blobs; this package reconciles that data into
// appliance-facing shapes but never becomes a second source of truth for
// it, per the plan's registry-boundary requirement.
package zotadapter

import (
	"context"
	"errors"
)

// ErrNotFound is returned by ListTags (and, where applicable, by future
// manifest-level lookups) when zot reports no such repository, so callers
// can distinguish "doesn't exist" from "zot is unreachable."
var ErrNotFound = errors.New("zotadapter: repository not found")

// Descriptor is one OCI content descriptor, as returned by zot's referrers
// API (an OCI Image Index's "manifests" entries).
type Descriptor struct {
	MediaType    string `json:"mediaType"`
	Digest       string `json:"digest"`
	Size         int64  `json:"size"`
	ArtifactType string `json:"artifactType,omitempty"`
}

// Client is the narrow contract the appliance control plane needs from the
// zot data plane: catalog, tag, and referrer listing, plus a health check.
// It never owns identity or RBAC; internal/httpapi combines this with
// internal/registryauth to filter results per caller.
type Client interface {
	// ListRepositories returns every repository name zot's catalog reports.
	ListRepositories(ctx context.Context) ([]string, error)

	// ListTags returns every tag zot reports for repository.
	ListTags(ctx context.Context, repository string) ([]string, error)

	// ListReferrers returns the OCI referrers of the manifest at digest
	// within repository.
	ListReferrers(ctx context.Context, repository, digest string) ([]Descriptor, error)

	// Health reports whether zot is currently reachable, for the
	// appliance's own dependency-status reporting.
	Health(ctx context.Context) error
}

package configuration

import (
	"context"
	"fmt"

	"appliance-code/services/controlplane/internal/storage"
)

// ArchiveKeyProvider resolves the purpose-separated key used to protect a
// portable configuration archive. Implementations may obtain it from a
// protected local file, an appliance secret, or a cloud key-management
// integration. The key is never persisted in the archive target or manifest.
type ArchiveKeyProvider interface {
	ConfigurationArchiveKey(ctx context.Context) (key []byte, keyID string, err error)
}

// ArchiveService coordinates archive bytes around the backend-neutral
// configuration repository. It deliberately does not know whether the live
// database is appliance-local PostgreSQL or a managed cloud service.
type ArchiveService struct {
	Revisions storage.ConfigurationRevisionStore
	Config    *Service
	Keys      ArchiveKeyProvider
	Target    ArchiveTarget
}

func (s ArchiveService) validateExportDependencies() error {
	if s.Revisions == nil || s.Keys == nil || s.Target == nil {
		return fmt.Errorf("configuration: archive revisions, key provider, and target are required")
	}
	return nil
}

// Export seals and stores one immutable revision. An empty revision ID means
// the current latest revision. Callers must select the target deliberately;
// this service never silently treats same-appliance object storage as an
// off-appliance backup.
func (s ArchiveService) Export(ctx context.Context, revisionID string) (ArchiveManifest, error) {
	if err := s.validateExportDependencies(); err != nil {
		return ArchiveManifest{}, err
	}
	var (
		revision storage.ConfigurationRevision
		err      error
	)
	if revisionID == "" {
		revision, err = s.Revisions.LatestConfigurationRevision(ctx)
	} else {
		revision, err = s.Revisions.GetConfigurationRevision(ctx, revisionID)
	}
	if err != nil {
		return ArchiveManifest{}, fmt.Errorf("configuration: load revision for archive: %w", err)
	}
	key, keyID, err := s.Keys.ConfigurationArchiveKey(ctx)
	if err != nil {
		return ArchiveManifest{}, fmt.Errorf("configuration: resolve archive key: %w", err)
	}
	data, manifest, err := Seal(revision, key, keyID)
	if err != nil {
		return ArchiveManifest{}, err
	}
	if err := s.Target.Put(ctx, manifest, data); err != nil {
		return ArchiveManifest{}, fmt.Errorf("configuration: store archive: %w", err)
	}
	return manifest, nil
}

// Verify reads and authenticates an archive without mutating a destination.
// It is required before an operator authorizes a restore/import operation.
func (s ArchiveService) Verify(ctx context.Context, archiveID string) (ArchiveManifest, error) {
	manifest, _, err := s.ReadVerified(ctx, archiveID)
	return manifest, err
}

// ListVerified returns only archives whose envelope authenticates with this
// appliance's archive key. Targets are untrusted storage: parsed manifests
// alone must never be presented as recoverable archives.
func (s ArchiveService) ListVerified(ctx context.Context) ([]ArchiveManifest, error) {
	if err := s.validateExportDependencies(); err != nil {
		return nil, err
	}
	candidates, err := s.Target.List(ctx)
	if err != nil {
		return nil, err
	}
	verified := make([]ArchiveManifest, 0, len(candidates))
	for _, candidate := range candidates {
		manifest, err := s.Verify(ctx, candidate.ArchiveID)
		if err != nil {
			return nil, fmt.Errorf("configuration: verify listed archive %s: %w", candidate.ArchiveID, err)
		}
		verified = append(verified, manifest)
	}
	return verified, nil
}

// ReadVerified returns the original sealed archive bytes only after their
// manifest and ciphertext authenticate with the configured archive key. It is
// used for an explicit operator download; callers must not expose bytes from
// an unverified target object.
func (s ArchiveService) ReadVerified(ctx context.Context, archiveID string) (ArchiveManifest, []byte, error) {
	if err := s.validateExportDependencies(); err != nil {
		return ArchiveManifest{}, nil, err
	}
	data, err := s.Target.Get(ctx, archiveID)
	if err != nil {
		return ArchiveManifest{}, nil, fmt.Errorf("configuration: read archive: %w", err)
	}
	key, keyID, err := s.Keys.ConfigurationArchiveKey(ctx)
	if err != nil {
		return ArchiveManifest{}, nil, fmt.Errorf("configuration: resolve archive key: %w", err)
	}
	manifest, _, err := Open(data, key, keyID)
	if err != nil {
		return ArchiveManifest{}, nil, err
	}
	return manifest, data, nil
}

// Import verifies/decrypts an archive before sending its canonical payload to
// the configuration import service. The latter rejects non-clean targets and
// validates staged metadata in the same database transaction.
func (s ArchiveService) Import(ctx context.Context, archiveID, actorID string) (storage.ConfigurationRevision, error) {
	if err := s.validateExportDependencies(); err != nil {
		return storage.ConfigurationRevision{}, err
	}
	if s.Config == nil {
		return storage.ConfigurationRevision{}, fmt.Errorf("configuration: archive import service is required")
	}
	data, err := s.Target.Get(ctx, archiveID)
	if err != nil {
		return storage.ConfigurationRevision{}, fmt.Errorf("configuration: read archive: %w", err)
	}
	key, keyID, err := s.Keys.ConfigurationArchiveKey(ctx)
	if err != nil {
		return storage.ConfigurationRevision{}, fmt.Errorf("configuration: resolve archive key: %w", err)
	}
	_, payload, err := Open(data, key, keyID)
	if err != nil {
		return storage.ConfigurationRevision{}, err
	}
	return s.Config.Import(ctx, payload, actorID)
}

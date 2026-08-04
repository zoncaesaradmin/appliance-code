package metadatabundle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/storage"
	"appliance-code/services/controlplane/internal/version"
)

var (
	ErrNotFound       = errors.New("metadatabundle: not found")
	ErrInvalidArchive = errors.New("metadatabundle: invalid archive")
	ErrInvalidSig     = errors.New("metadatabundle: untrusted signature")
)

// Service owns active metadata loading, install, and rollback.
type Service struct {
	mu       sync.RWMutex
	active   *Bundle
	store    storage.MetadataBundleStore
	db       storage.DB
	audit    *audit.Recorder
	dataDir  string
	software string
	now      func() time.Time
}

func NewService(db storage.DB, store storage.MetadataBundleStore, recorder *audit.Recorder, dataDir string) (*Service, error) {
	s := &Service{
		db:       db,
		store:    store,
		audit:    recorder,
		dataDir:  dataDir,
		software: version.Version,
		now:      time.Now,
	}
	if err := s.ensureActive(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Service) Active() *Bundle {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	activeRec, err := s.store.GetMetadataBundle(ctx, "active")
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return Status{}, err
	}
	prevRec, err := s.store.GetMetadataBundle(ctx, "previous")
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return Status{}, err
	}
	st := Status{
		SoftwareVersion: s.software,
		CanRollback:     prevRec.MetadataVersion != "",
	}
	if activeRec.MetadataVersion != "" {
		st.ActiveMetadataVersion = activeRec.MetadataVersion
		st.ActiveDigest = activeRec.Digest
		st.DirectoryName = activeRec.DirectoryName
	} else if s.Active() != nil {
		st.ActiveMetadataVersion = s.Active().Manifest.Metadata.MetadataVersion
		st.DirectoryName = DirectoryName(st.ActiveMetadataVersion)
	}
	if prevRec.MetadataVersion != "" {
		st.PreviousMetadataVersion = prevRec.MetadataVersion
		st.PreviousDigest = prevRec.Digest
	}
	return st, nil
}

func (s *Service) ValidateArchive(ctx context.Context, archivePath, signature string) (ValidationResult, *Bundle, error) {
	if err := validateSignature(signature); err != nil {
		return ValidationResult{}, nil, err
	}
	tmp, err := os.MkdirTemp(s.dataDir, "metadata-validate-*")
	if err != nil {
		return ValidationResult{}, nil, err
	}
	defer os.RemoveAll(tmp)
	dir, err := ExtractArchive(archivePath, tmp)
	if err != nil {
		return ValidationResult{}, nil, fmt.Errorf("%w: %v", ErrInvalidArchive, err)
	}
	b, err := LoadDirectory(dir)
	if err != nil {
		return ValidationResult{OK: false, Groups: []ValidationGroup{{
			Name: "schema", OK: false, Message: "Invalid metadata bundle", Errors: []string{err.Error()},
		}}}, nil, nil
	}
	return ValidateForSoftware(b, s.software), b, nil
}

func (s *Service) InstallArchive(ctx context.Context, actor audit.Actor, archivePath, signature string) (Status, ValidationResult, error) {
	validation, b, err := s.ValidateArchive(ctx, archivePath, signature)
	if err != nil {
		return Status{}, ValidationResult{}, err
	}
	if !validation.OK || b == nil {
		return Status{}, validation, ErrInvalidArchive
	}
	digest, err := fileDigest(archivePath)
	if err != nil {
		return Status{}, validation, err
	}
	destRoot := filepath.Join(s.dataDir, "metadata-bundles")
	if err := os.MkdirAll(destRoot, 0o755); err != nil {
		return Status{}, validation, err
	}
	dirName := DirectoryName(b.Manifest.Metadata.MetadataVersion)
	dest := filepath.Join(destRoot, dirName)
	_ = os.RemoveAll(dest)
	extracted, err := ExtractArchive(archivePath, destRoot)
	if err != nil {
		return Status{}, validation, err
	}
	if filepath.Base(extracted) != dirName {
		// normalize name
		_ = os.RemoveAll(dest)
		if err := os.Rename(extracted, dest); err != nil {
			return Status{}, validation, err
		}
		extracted = dest
	}
	loaded, err := LoadDirectory(extracted)
	if err != nil {
		return Status{}, validation, err
	}
	err = s.db.WithTx(ctx, func(txCtx context.Context) error {
		if current, err := s.store.GetMetadataBundle(txCtx, "active"); err == nil {
			current.Slot = "previous"
			if err := s.store.PutMetadataBundle(txCtx, current); err != nil {
				return err
			}
		} else if !errors.Is(err, storage.ErrNotFound) {
			return err
		}
		rec := storage.MetadataBundleRecord{
			Slot:            "active",
			MetadataVersion: loaded.Manifest.Metadata.MetadataVersion,
			SoftwareVersion: loaded.Manifest.Metadata.SoftwareVersion,
			Digest:          digest,
			DirectoryName:   dirName,
			DirectoryPath:   extracted,
			Signature:       signature,
			InstalledAt:     s.now().UTC(),
			InstalledBy:     actor.UserID,
		}
		if err := s.store.PutMetadataBundle(txCtx, rec); err != nil {
			return err
		}
		if s.audit != nil {
			return s.audit.Record(txCtx, actor, audit.Event{
				Action:     "metadata_bundle.install",
				TargetType: "metadata_bundle",
				TargetID:   rec.MetadataVersion,
				Outcome:    storage.AuditOutcomeSuccess,
				Details: map[string]any{
					"digest": digest,
				},
			})
		}
		return nil
	})
	if err != nil {
		return Status{}, validation, err
	}
	s.mu.Lock()
	s.active = loaded
	s.mu.Unlock()
	st, err := s.Status(ctx)
	return st, validation, err
}

func (s *Service) Rollback(ctx context.Context, actor audit.Actor) (Status, error) {
	prev, err := s.store.GetMetadataBundle(ctx, "previous")
	if errors.Is(err, storage.ErrNotFound) {
		return Status{}, fmt.Errorf("metadatabundle: no previous metadata bundle to roll back to")
	}
	if err != nil {
		return Status{}, err
	}
	if err := CompatibleWithSoftware(s.software, prev.MetadataVersion); err != nil {
		return Status{}, err
	}
	loaded, err := LoadDirectory(prev.DirectoryPath)
	if err != nil {
		return Status{}, err
	}
	err = s.db.WithTx(ctx, func(txCtx context.Context) error {
		active, err := s.store.GetMetadataBundle(txCtx, "active")
		if err != nil {
			return err
		}
		// swap
		prev.Slot = "active"
		active.Slot = "previous"
		if err := s.store.PutMetadataBundle(txCtx, prev); err != nil {
			return err
		}
		if err := s.store.PutMetadataBundle(txCtx, active); err != nil {
			return err
		}
		if s.audit != nil {
			return s.audit.Record(txCtx, actor, audit.Event{
				Action:     "metadata_bundle.rollback",
				TargetType: "metadata_bundle",
				TargetID:   prev.MetadataVersion,
				Outcome:    storage.AuditOutcomeSuccess,
			})
		}
		return nil
	})
	if err != nil {
		return Status{}, err
	}
	s.mu.Lock()
	s.active = loaded
	s.mu.Unlock()
	return s.Status(ctx)
}

func (s *Service) ensureActive(ctx context.Context) error {
	if rec, err := s.store.GetMetadataBundle(ctx, "active"); err == nil {
		b, err := LoadDirectory(rec.DirectoryPath)
		if err == nil && CompatibleWithSoftware(s.software, rec.MetadataVersion) == nil {
			s.active = b
			return nil
		}
	}
	baseVer, err := BaseMetadataVersion(s.software)
	if err != nil {
		return err
	}
	destRoot := filepath.Join(s.dataDir, "metadata-bundles")
	dirName := DirectoryName(baseVer)
	dest := filepath.Join(destRoot, dirName)
	if err := os.MkdirAll(destRoot, 0o755); err != nil {
		return err
	}
	// Prefer a host-seeded tree (zonctl extract onto the hostPath mount)
	// over re-materializing the embedded base policy.
	if b, err := LoadDirectory(dest); err == nil && CompatibleWithSoftware(s.software, b.Manifest.Metadata.MetadataVersion) == nil {
		digest, _ := dirDigest(dest)
		rec := storage.MetadataBundleRecord{
			Slot:            "active",
			MetadataVersion: b.Manifest.Metadata.MetadataVersion,
			SoftwareVersion: b.Manifest.Metadata.SoftwareVersion,
			Digest:          digest,
			DirectoryName:   dirName,
			DirectoryPath:   dest,
			Signature:       "offline-dev",
			InstalledAt:     s.now().UTC(),
			InstalledBy:     "system",
		}
		if err := s.store.PutMetadataBundle(ctx, rec); err != nil {
			return err
		}
		s.active = b
		return nil
	}
	if err := materializeEmbedded(dest, s.software, baseVer); err != nil {
		return err
	}
	b, err := LoadDirectory(dest)
	if err != nil {
		return err
	}
	digest, _ := dirDigest(dest)
	rec := storage.MetadataBundleRecord{
		Slot:            "active",
		MetadataVersion: baseVer,
		SoftwareVersion: b.Manifest.Metadata.SoftwareVersion,
		Digest:          digest,
		DirectoryName:   dirName,
		DirectoryPath:   dest,
		Signature:       "offline-dev",
		InstalledAt:     s.now().UTC(),
		InstalledBy:     "system",
	}
	if err := s.store.PutMetadataBundle(ctx, rec); err != nil {
		return err
	}
	s.active = b
	return nil
}

func validateSignature(signature string) error {
	sig := strings.TrimSpace(signature)
	if sig == "" {
		return fmt.Errorf("%w: signature is required", ErrInvalidSig)
	}
	if sig != "offline-dev" && !strings.HasPrefix(sig, "offline:") {
		return fmt.Errorf("%w", ErrInvalidSig)
	}
	return nil
}

func fileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func dirDigest(dir string) (string, error) {
	h := sha256.New()
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		fmt.Fprintf(h, "%s\n", filepath.ToSlash(rel))
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(h, f)
		f.Close()
		return copyErr
	})
	if err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

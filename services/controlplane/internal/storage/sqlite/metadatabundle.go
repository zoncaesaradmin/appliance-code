package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"appliance-code/services/controlplane/internal/storage"
)

type MetadataBundleStore struct {
	db *DB
}

func NewMetadataBundleStore(db *DB) *MetadataBundleStore {
	return &MetadataBundleStore{db: db}
}

func (s *MetadataBundleStore) GetMetadataBundle(ctx context.Context, slot string) (storage.MetadataBundleRecord, error) {
	row := s.db.q(ctx).QueryRowContext(ctx, `
		SELECT slot, metadata_version, software_version, digest, directory_name, directory_path,
		       signature, installed_at, installed_by
		FROM metadata_bundle_state WHERE slot = ?`, slot)
	var (
		rec         storage.MetadataBundleRecord
		installedAt string
	)
	err := row.Scan(&rec.Slot, &rec.MetadataVersion, &rec.SoftwareVersion, &rec.Digest, &rec.DirectoryName,
		&rec.DirectoryPath, &rec.Signature, &installedAt, &rec.InstalledBy)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.MetadataBundleRecord{}, storage.ErrNotFound
	}
	if err != nil {
		return storage.MetadataBundleRecord{}, fmt.Errorf("sqlite: get metadata bundle %s: %w", slot, err)
	}
	rec.InstalledAt, err = parseTimeFlexible(installedAt)
	if err != nil {
		return storage.MetadataBundleRecord{}, err
	}
	return rec, nil
}

func (s *MetadataBundleStore) PutMetadataBundle(ctx context.Context, rec storage.MetadataBundleRecord) error {
	if rec.InstalledAt.IsZero() {
		rec.InstalledAt = time.Now().UTC()
	}
	_, err := s.db.q(ctx).ExecContext(ctx, `
		INSERT INTO metadata_bundle_state
			(slot, metadata_version, software_version, digest, directory_name, directory_path, signature, installed_at, installed_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(slot) DO UPDATE SET
			metadata_version = excluded.metadata_version,
			software_version = excluded.software_version,
			digest = excluded.digest,
			directory_name = excluded.directory_name,
			directory_path = excluded.directory_path,
			signature = excluded.signature,
			installed_at = excluded.installed_at,
			installed_by = excluded.installed_by`,
		rec.Slot, rec.MetadataVersion, rec.SoftwareVersion, rec.Digest, rec.DirectoryName, rec.DirectoryPath,
		rec.Signature, rec.InstalledAt.UTC().Format(time.RFC3339Nano), rec.InstalledBy,
	)
	if err != nil {
		return fmt.Errorf("sqlite: put metadata bundle %s: %w", rec.Slot, err)
	}
	return nil
}

func (s *MetadataBundleStore) ClearMetadataBundle(ctx context.Context, slot string) error {
	_, err := s.db.q(ctx).ExecContext(ctx, `DELETE FROM metadata_bundle_state WHERE slot = ?`, slot)
	if err != nil {
		return fmt.Errorf("sqlite: clear metadata bundle %s: %w", slot, err)
	}
	return nil
}

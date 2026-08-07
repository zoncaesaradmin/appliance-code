package sqlite

import (
	"context"
	"fmt"
	"time"

	"appliance-code/services/controlplane/internal/storage"
)

// BuilderCatalogStore is the SQLite-backed storage.BuilderCatalogStore.
type BuilderCatalogStore struct {
	db *DB
}

// NewBuilderCatalogStore returns a BuilderCatalogStore backed by db.
func NewBuilderCatalogStore(db *DB) *BuilderCatalogStore {
	return &BuilderCatalogStore{db: db}
}

func (s *BuilderCatalogStore) GetBuilderCatalog(ctx context.Context) (storage.BuilderCatalogRecord, error) {
	row := s.db.q(ctx).QueryRowContext(ctx, `
		SELECT document_text, content_type, updated_at
		FROM builder_catalog WHERE id = 1`)
	var (
		rec       storage.BuilderCatalogRecord
		updatedAt string
	)
	if err := row.Scan(&rec.DocumentText, &rec.ContentType, &updatedAt); err != nil {
		return storage.BuilderCatalogRecord{}, fmt.Errorf("sqlite: getting builder catalog: %w", err)
	}
	var err error
	if rec.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt); err != nil {
		if rec.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt); err != nil {
			return storage.BuilderCatalogRecord{}, fmt.Errorf("sqlite: parsing builder catalog updated_at: %w", err)
		}
	}
	return rec, nil
}

func (s *BuilderCatalogStore) PutBuilderCatalog(ctx context.Context, rec storage.BuilderCatalogRecord) error {
	if rec.UpdatedAt.IsZero() {
		rec.UpdatedAt = time.Now().UTC()
	}
	if rec.ContentType == "" {
		rec.ContentType = "application/json"
	}
	_, err := s.db.q(ctx).ExecContext(ctx, `
		UPDATE builder_catalog
		SET document_text = ?, content_type = ?, updated_at = ?
		WHERE id = 1`,
		rec.DocumentText, rec.ContentType, rec.UpdatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("sqlite: putting builder catalog: %w", err)
	}
	return nil
}

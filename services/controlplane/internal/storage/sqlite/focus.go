package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"appliance-code/services/controlplane/internal/storage"
)

type focusContentStore struct{ db *DB }

func NewFocusContentStore(db *DB) storage.FocusContentStore { return &focusContentStore{db: db} }

func (s *focusContentStore) GetFocusContent(ctx context.Context) (storage.FocusContent, error) {
	var content storage.FocusContent
	var publishedAt string
	err := s.db.q(ctx).QueryRowContext(ctx, `SELECT resource_type, resource_path, title, message, published_at, published_by FROM focus_content WHERE id=1`).Scan(
		&content.ResourceType, &content.ResourcePath, &content.Title, &content.Message, &publishedAt, &content.PublishedBy)
	if err == sql.ErrNoRows {
		return storage.FocusContent{}, storage.ErrNotFound
	}
	if err != nil {
		return storage.FocusContent{}, fmt.Errorf("sqlite: getting focus content: %w", err)
	}
	content.PublishedAt, _ = time.Parse(time.RFC3339Nano, publishedAt)
	return content, nil
}

func (s *focusContentStore) PutFocusContent(ctx context.Context, content storage.FocusContent) error {
	_, err := s.db.q(ctx).ExecContext(ctx, `INSERT INTO focus_content(id, resource_type, resource_path, title, message, published_at, published_by)
        VALUES (1, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET resource_type=excluded.resource_type, resource_path=excluded.resource_path, title=excluded.title, message=excluded.message, published_at=excluded.published_at, published_by=excluded.published_by`,
		content.ResourceType, content.ResourcePath, content.Title, content.Message, content.PublishedAt.UTC().Format(time.RFC3339Nano), content.PublishedBy)
	if err != nil {
		return fmt.Errorf("sqlite: saving focus content: %w", err)
	}
	return nil
}

func (s *focusContentStore) ClearFocusContent(ctx context.Context) error {
	if _, err := s.db.q(ctx).ExecContext(ctx, `DELETE FROM focus_content WHERE id=1`); err != nil {
		return fmt.Errorf("sqlite: clearing focus content: %w", err)
	}
	return nil
}

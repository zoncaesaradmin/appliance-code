package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"appliance-code/server/backend/internal/storage"
)

// IdempotencyStore is the SQLite-backed storage.IdempotencyStore.
type IdempotencyStore struct {
	db *DB
}

// NewIdempotencyStore returns an IdempotencyStore backed by db.
func NewIdempotencyStore(db *DB) *IdempotencyStore {
	return &IdempotencyStore{db: db}
}

func (s *IdempotencyStore) Reserve(ctx context.Context, scope, key, requestHash string, ttl time.Duration) (storage.IdempotencyRecord, bool, error) {
	var claimed bool
	var existing storage.IdempotencyRecord

	err := s.db.WithTx(ctx, func(ctx context.Context) error {
		row := s.db.q(ctx).QueryRowContext(ctx, `
			SELECT scope, key, request_hash, response_status, response_body, created_at, expires_at
			FROM idempotency_records WHERE scope = ? AND key = ?`, scope, key)

		rec, err := scanIdempotency(row)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			now := time.Now().UTC()
			_, err := s.db.q(ctx).ExecContext(ctx, `
				INSERT INTO idempotency_records (scope, key, request_hash, created_at, expires_at)
				VALUES (?, ?, ?, ?, ?)`,
				scope, key, requestHash, now.Format(time.RFC3339Nano), now.Add(ttl).Format(time.RFC3339Nano),
			)
			if err != nil {
				return fmt.Errorf("sqlite: reserving idempotency key: %w", err)
			}
			claimed = true
			return nil
		case err != nil:
			return fmt.Errorf("sqlite: reading idempotency key: %w", err)
		default:
			existing = rec
			claimed = false
			return nil
		}
	})
	if err != nil {
		return storage.IdempotencyRecord{}, false, err
	}
	return existing, claimed, nil
}

func (s *IdempotencyStore) Complete(ctx context.Context, scope, key string, status int, body []byte) error {
	res, err := s.db.q(ctx).ExecContext(ctx, `
		UPDATE idempotency_records SET response_status = ?, response_body = ?
		WHERE scope = ? AND key = ?`, status, body, scope, key)
	if err != nil {
		return fmt.Errorf("sqlite: completing idempotency key: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: checking idempotency update result: %w", err)
	}
	if n == 0 {
		return storage.ErrNotFound
	}
	return nil
}

func scanIdempotency(row *sql.Row) (storage.IdempotencyRecord, error) {
	var (
		rec                  storage.IdempotencyRecord
		status               sql.NullInt64
		body                 []byte
		createdAt, expiresAt string
	)
	if err := row.Scan(&rec.Scope, &rec.Key, &rec.RequestHash, &status, &body, &createdAt, &expiresAt); err != nil {
		return storage.IdempotencyRecord{}, err
	}
	rec.ResponseStatus = int(status.Int64)
	rec.ResponseBody = body
	var err error
	if rec.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return storage.IdempotencyRecord{}, err
	}
	if rec.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiresAt); err != nil {
		return storage.IdempotencyRecord{}, err
	}
	return rec, nil
}

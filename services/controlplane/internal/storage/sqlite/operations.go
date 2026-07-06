package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"appliance-code/services/controlplane/internal/storage"
)

// OperationsStore is the SQLite-backed storage.OperationsStore.
type OperationsStore struct {
	db *DB
}

// NewOperationsStore returns an OperationsStore backed by db.
func NewOperationsStore(db *DB) *OperationsStore {
	return &OperationsStore{db: db}
}

func (s *OperationsStore) Create(ctx context.Context, op storage.Operation) error {
	now := time.Now().UTC()
	if op.CreatedAt.IsZero() {
		op.CreatedAt = now
	}
	if op.UpdatedAt.IsZero() {
		op.UpdatedAt = now
	}

	_, err := s.db.q(ctx).ExecContext(ctx, `
		INSERT INTO operations (id, kind, owner_id, status, result_body, problem_body, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		op.ID, string(op.Kind), nullableString(op.OwnerID), string(op.Status),
		op.ResultBody, op.ProblemBody,
		op.CreatedAt.Format(time.RFC3339Nano), op.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("sqlite: creating operation %s: %w", op.ID, err)
	}
	return nil
}

func (s *OperationsStore) Get(ctx context.Context, id string) (storage.Operation, error) {
	row := s.db.q(ctx).QueryRowContext(ctx, `
		SELECT id, kind, owner_id, status, result_body, problem_body, created_at, updated_at
		FROM operations WHERE id = ?`, id)

	var (
		op                   storage.Operation
		kind, status         string
		ownerID              sql.NullString
		createdAt, updatedAt string
	)
	if err := row.Scan(&op.ID, &kind, &ownerID, &status, &op.ResultBody, &op.ProblemBody, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.Operation{}, storage.ErrNotFound
		}
		return storage.Operation{}, fmt.Errorf("sqlite: getting operation %s: %w", id, err)
	}

	op.Kind = storage.OperationKind(kind)
	op.Status = storage.OperationStatus(status)
	op.OwnerID = ownerID.String
	var err error
	if op.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return storage.Operation{}, fmt.Errorf("sqlite: parsing created_at for operation %s: %w", id, err)
	}
	if op.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt); err != nil {
		return storage.Operation{}, fmt.Errorf("sqlite: parsing updated_at for operation %s: %w", id, err)
	}
	return op, nil
}

func (s *OperationsStore) UpdateStatus(ctx context.Context, id string, status storage.OperationStatus, resultBody, problemBody []byte) error {
	res, err := s.db.q(ctx).ExecContext(ctx, `
		UPDATE operations SET status = ?, result_body = ?, problem_body = ?, updated_at = ?
		WHERE id = ?`,
		string(status), resultBody, problemBody, time.Now().UTC().Format(time.RFC3339Nano), id,
	)
	if err != nil {
		return fmt.Errorf("sqlite: updating operation %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: checking update result for operation %s: %w", id, err)
	}
	if n == 0 {
		return storage.ErrNotFound
	}
	return nil
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

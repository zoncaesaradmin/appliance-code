package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"appliance-code/services/controlplane/internal/storage"
)

// ThrottleStore is the SQLite-backed storage.ThrottleStore, giving login
// failure counters a durable home so restarts don't reset lockout state,
// per ADR 0010.
type ThrottleStore struct {
	db *DB
}

// NewThrottleStore returns a ThrottleStore backed by db.
func NewThrottleStore(db *DB) *ThrottleStore {
	return &ThrottleStore{db: db}
}

func (s *ThrottleStore) Get(ctx context.Context, username string) (storage.ThrottleState, error) {
	row := s.db.q(ctx).QueryRowContext(ctx, `
		SELECT username, failure_count, first_failure_at, last_failure_at, locked_until
		FROM auth_throttle_state WHERE username = ?`, username)
	st, err := scanThrottleState(row)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.ThrottleState{Username: username}, nil
	}
	if err != nil {
		return storage.ThrottleState{}, fmt.Errorf("sqlite: getting throttle state for %s: %w", username, err)
	}
	return st, nil
}

func scanThrottleState(row interface{ Scan(dest ...any) error }) (storage.ThrottleState, error) {
	var (
		st                                         storage.ThrottleState
		firstFailureAt, lastFailureAt, lockedUntil sql.NullString
	)
	if err := row.Scan(&st.Username, &st.FailureCount, &firstFailureAt, &lastFailureAt, &lockedUntil); err != nil {
		return storage.ThrottleState{}, err
	}
	var err error
	if firstFailureAt.Valid {
		if st.FirstFailureAt, err = time.Parse(time.RFC3339Nano, firstFailureAt.String); err != nil {
			return storage.ThrottleState{}, err
		}
	}
	if lastFailureAt.Valid {
		if st.LastFailureAt, err = time.Parse(time.RFC3339Nano, lastFailureAt.String); err != nil {
			return storage.ThrottleState{}, err
		}
	}
	if lockedUntil.Valid {
		if st.LockedUntil, err = time.Parse(time.RFC3339Nano, lockedUntil.String); err != nil {
			return storage.ThrottleState{}, err
		}
	}
	return st, nil
}

// RecordFailure increments the durable failure counter for username,
// resetting it first if the previous failure window has expired, and sets
// LockedUntil once failureCount reaches lockThreshold.
func (s *ThrottleStore) RecordFailure(ctx context.Context, username string, now time.Time, windowReset, lockDuration time.Duration, lockThreshold int) (storage.ThrottleState, error) {
	var result storage.ThrottleState
	err := s.db.WithTx(ctx, func(ctx context.Context) error {
		current, err := s.Get(ctx, username)
		if err != nil {
			return err
		}

		if current.FirstFailureAt.IsZero() || now.Sub(current.FirstFailureAt) > windowReset {
			current.FirstFailureAt = now
			current.FailureCount = 1
		} else {
			current.FailureCount++
		}
		current.LastFailureAt = now
		current.Username = username

		if current.FailureCount >= lockThreshold {
			current.LockedUntil = now.Add(lockDuration)
		}

		var lockedUntil any
		if !current.LockedUntil.IsZero() {
			lockedUntil = current.LockedUntil.Format(time.RFC3339Nano)
		}

		_, err = s.db.q(ctx).ExecContext(ctx, `
			INSERT INTO auth_throttle_state (username, failure_count, first_failure_at, last_failure_at, locked_until)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (username) DO UPDATE SET
				failure_count = excluded.failure_count,
				first_failure_at = excluded.first_failure_at,
				last_failure_at = excluded.last_failure_at,
				locked_until = excluded.locked_until`,
			username, current.FailureCount, current.FirstFailureAt.Format(time.RFC3339Nano),
			current.LastFailureAt.Format(time.RFC3339Nano), lockedUntil,
		)
		if err != nil {
			return fmt.Errorf("sqlite: recording login failure for %s: %w", username, err)
		}
		result = current
		return nil
	})
	return result, err
}

func (s *ThrottleStore) Reset(ctx context.Context, username string) error {
	_, err := s.db.q(ctx).ExecContext(ctx, `DELETE FROM auth_throttle_state WHERE username = ?`, username)
	if err != nil {
		return fmt.Errorf("sqlite: resetting throttle state for %s: %w", username, err)
	}
	return nil
}

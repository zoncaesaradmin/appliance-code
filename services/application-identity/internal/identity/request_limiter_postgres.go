package identity

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// PostgresRequestLimiter enforces rolling per-client/action limits across
// replicas sharing the same database, HMAC secret, window, and limits.
type PostgresRequestLimiter struct {
	db     *sql.DB
	key    []byte
	window time.Duration
	limits map[string]int
}

func NewPostgresRequestLimiter(db *sql.DB, key []byte, window time.Duration, limits map[string]int) (*PostgresRequestLimiter, error) {
	if db == nil || len(key) < 32 || window < time.Microsecond || window > 24*time.Hour || len(limits) == 0 {
		return nil, errors.New("invalid postgres request limiter configuration")
	}
	copied := make(map[string]int, len(limits))
	for action, limit := range limits {
		if action == "" || len(action) > 256 || strings.ContainsRune(action, 0) || limit < 1 || limit > 10000 {
			return nil, errors.New("invalid postgres request limit")
		}
		copied[action] = limit
	}
	return &PostgresRequestLimiter{db: db, key: append([]byte(nil), key...), window: window, limits: copied}, nil
}

func (l *PostgresRequestLimiter) Allow(ctx context.Context, client, action string) (bool, error) {
	limit, ok := l.limits[action]
	if !ok || client == "" || len(client) > 256 || strings.ContainsRune(client, 0) {
		return false, errors.New("invalid request limit key")
	}
	mac := hmac.New(sha256.New, l.key)
	_, _ = mac.Write([]byte("http-request\x00" + action + "\x00" + client))
	bucket := mac.Sum(nil)
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin request limit transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "zonited:http-limit:"+hex.EncodeToString(bucket)); err != nil {
		return false, fmt.Errorf("lock request limit bucket: %w", err)
	}
	// clock_timestamp is evaluated after lock acquisition, unlike the
	// transaction-start timestamp, which may be stale after waiting for a lock.
	var now time.Time
	if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return false, fmt.Errorf("read request limit clock: %w", err)
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM identity_request_attempts WHERE bucket_key=$1 AND created_at > $2`, bucket, now.Add(-l.window)).Scan(&count); err != nil {
		return false, fmt.Errorf("count request attempts: %w", err)
	}
	if count >= limit {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO identity_request_attempts (bucket_key, created_at) VALUES ($1,$2)`, bucket, now); err != nil {
		return false, fmt.Errorf("record request attempt: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit request limit: %w", err)
	}
	return true, nil
}

// Prune removes at most batchSize expired attempts. A 24-hour minimum protects
// every window accepted by this implementation, including rolling deployments
// with different configuration. It must be scheduled independently.
func (l *PostgresRequestLimiter) Prune(ctx context.Context, retention time.Duration, batchSize int) (int64, error) {
	if retention < 24*time.Hour || batchSize < 1 || batchSize > 10000 {
		return 0, errors.New("invalid request limit retention")
	}
	result, err := l.db.ExecContext(ctx, `DELETE FROM identity_request_attempts WHERE id IN (SELECT id FROM identity_request_attempts WHERE created_at < clock_timestamp() - ($1::bigint * interval '1 microsecond') ORDER BY created_at LIMIT $2)`, retention.Microseconds(), batchSize)
	if err != nil {
		return 0, fmt.Errorf("prune request attempts: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count pruned request attempts: %w", err)
	}
	return count, nil
}

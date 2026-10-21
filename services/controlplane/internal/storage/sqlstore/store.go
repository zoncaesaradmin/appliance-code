// Package sqlstore implements the control-plane storage interfaces over SQL.
// Appliance and cloud deployments use PostgreSQL; SQLite is retained solely
// for local development and isolated tests behind the same repository and
// transaction contracts.
package sqlstore

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver
)

// DB is the dialect-aware repository backing connection. Its SQLite opening
// path is development-only; appliance runtime uses OpenPostgres.
type DB struct {
	sqlDB    *sql.DB
	path     string
	postgres bool
}

// OpenPostgres opens the PostgreSQL implementation behind the repository
// contracts. No SQLite driver or file is used by this path.
func OpenPostgres(ctx context.Context, dsn string, maxOpenConns, maxIdleConns int, maxLifetime time.Duration) (*DB, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("postgres: DSN is required")
	}
	if maxOpenConns <= 0 || maxIdleConns < 0 || maxIdleConns > maxOpenConns {
		return nil, fmt.Errorf("postgres: invalid connection pool limits")
	}
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: opening database: %w", err)
	}
	sqlDB.SetMaxOpenConns(maxOpenConns)
	sqlDB.SetMaxIdleConns(maxIdleConns)
	sqlDB.SetConnMaxLifetime(maxLifetime)
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("postgres: pinging database: %w", err)
	}
	return &DB{sqlDB: sqlDB, postgres: true}, nil
}

type txKey struct{}

// OpenSQLite creates the data directory if needed, opens the SQLite file at path,
// and configures it for single-writer appliance use: foreign keys on, WAL
// journaling, a bounded busy timeout, and a single open connection so SQLite
// itself serializes writers instead of returning spurious "database is
// locked" errors under Go's connection pool.
func OpenSQLite(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("sqlite: creating data directory: %w", err)
	}

	dsn := fmt.Sprintf(
		"file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)",
		path,
	)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite: opening %s: %w", path, err)
	}
	// SQLite has a single writer regardless of connection count; capping the
	// pool at one connection makes Go's pool serialize writers instead of
	// each fighting over file locks under the configured busy timeout.
	sqlDB.SetMaxOpenConns(1)

	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("sqlite: pinging %s: %w", path, err)
	}

	return &DB{sqlDB: sqlDB, path: path}, nil
}

// WithTx runs fn inside a single transaction, storing it in ctx so
// repository methods called from fn transparently participate in it.
// Committing happens only if fn returns nil; any error, including a panic
// recovered and re-raised by the caller, rolls back.
func (db *DB) WithTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, alreadyInTx := ctx.Value(txKey{}).(*sql.Tx); alreadyInTx {
		return fn(ctx)
	}

	tx, err := db.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: beginning transaction: %w", err)
	}

	txCtx := context.WithValue(ctx, txKey{}, tx)
	if err := fn(txCtx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("%w (rollback also failed: %v)", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite: committing transaction: %w", err)
	}
	return nil
}

// querier is satisfied by both *sql.DB and *sql.Tx.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// q returns the active transaction from ctx if WithTx started one, or the
// pooled *sql.DB otherwise, so repository code always writes through
// whichever is correct without needing to know which one is active.
func (db *DB) q(ctx context.Context) querier {
	var q querier
	if tx, ok := ctx.Value(txKey{}).(*sql.Tx); ok {
		q = tx
	} else {
		q = db.sqlDB
	}
	if db.postgres {
		return postgresQuerier{querier: q}
	}
	return q
}

type postgresQuerier struct{ querier }

func (q postgresQuerier) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return q.querier.ExecContext(ctx, postgresPlaceholders(query), args...)
}

func (q postgresQuerier) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return q.querier.QueryContext(ctx, postgresPlaceholders(query), args...)
}

func (q postgresQuerier) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return q.querier.QueryRowContext(ctx, postgresPlaceholders(query), args...)
}

// postgresPlaceholders rewrites repository positional placeholders while
// preserving question marks inside single-quoted SQL literals.
func postgresPlaceholders(query string) string {
	var out strings.Builder
	out.Grow(len(query) + 8)
	inQuote := false
	index := 0
	for _, r := range query {
		if r == '\'' {
			inQuote = !inQuote
		}
		if r == '?' && !inQuote {
			index++
			out.WriteByte('$')
			out.WriteString(strconv.Itoa(index))
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

// Ping reports whether the database is reachable and accepting queries, for
// the readiness endpoint.
func (db *DB) Ping(ctx context.Context) error {
	return db.sqlDB.PingContext(ctx)
}

// Backup writes a transactionally consistent snapshot to destPath using
// SQLite's VACUUM INTO, which produces a defragmented, lock-consistent copy
// without requiring CGo-only backup-API bindings. destPath must not already
// exist.
func (db *DB) Backup(ctx context.Context, destPath string) error {
	if db.postgres {
		return fmt.Errorf("postgres: database backup is provided by the coordinated PostgreSQL recovery-set implementation")
	}
	if _, err := os.Stat(destPath); err == nil {
		return fmt.Errorf("sqlite: backup destination %s already exists", destPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("sqlite: checking backup destination: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0o750); err != nil {
		return fmt.Errorf("sqlite: creating backup directory: %w", err)
	}

	if _, err := db.sqlDB.ExecContext(ctx, `VACUUM INTO ?`, destPath); err != nil {
		return fmt.Errorf("sqlite: backing up to %s: %w", destPath, err)
	}
	return nil
}

// Close releases the underlying connection pool.
func (db *DB) Close() error {
	return db.sqlDB.Close()
}

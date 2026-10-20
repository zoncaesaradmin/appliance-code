// Package postgres owns the PostgreSQL control-plane database lifecycle.
// Store implementations are connected in a later change; this package keeps
// connection, transaction, readiness, and migration coordination separate
// from application wiring.
package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"appliance-code/services/controlplane/internal/storage"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// Config contains the narrow connection policy the control plane needs.
// Credentials remain in the DSN, which must come from a mounted secret and
// must never be logged.
type Config struct {
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// DB implements storage.DB for PostgreSQL. It is intentionally not selected
// by application configuration until the full PostgreSQL store set and
// native schema migrations are delivered.
type DB struct{ sqlDB *sql.DB }

var _ storage.DB = (*DB)(nil)

func Open(ctx context.Context, cfg Config) (*DB, error) {
	if strings.TrimSpace(cfg.DSN) == "" {
		return nil, fmt.Errorf("postgres: DSN is required")
	}
	if cfg.MaxOpenConns <= 0 || cfg.MaxIdleConns < 0 || cfg.MaxIdleConns > cfg.MaxOpenConns {
		return nil, fmt.Errorf("postgres: invalid connection pool limits")
	}
	db, err := sql.Open("pgx", cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("postgres: opening database: %w", err)
	}
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("postgres: pinging database: %w", err)
	}
	return &DB{sqlDB: db}, nil
}

// Migrate will own PostgreSQL-native migration application. Refusing to run
// until those migrations are registered is deliberate: silently creating a
// partial schema would be unsafe during the SQLite-to-PostgreSQL transition.
func (db *DB) Migrate(context.Context) error {
	return fmt.Errorf("postgres: native control-plane migrations are not registered")
}

func (db *DB) WithTx(ctx context.Context, fn func(context.Context) error) error {
	tx, err := db.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("postgres: beginning transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit
	if err := fn(context.WithValue(ctx, txKey{}, tx)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("postgres: committing transaction: %w", err)
	}
	return nil
}

func (db *DB) Ping(ctx context.Context) error { return db.sqlDB.PingContext(ctx) }

func (db *DB) Backup(context.Context, string) error {
	return fmt.Errorf("postgres: backup requires the coordinated recovery-set implementation")
}

func (db *DB) Close() error { return db.sqlDB.Close() }

type txKey struct{}

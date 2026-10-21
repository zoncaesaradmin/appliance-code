package sqlstore

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type migration struct {
	Version int
	Name    string
	SQL     string
}

// migrationExecutor is satisfied by both *sql.DB and *sql.Conn. PostgreSQL
// migration coordination deliberately uses one *sql.Conn because advisory
// locks are session-scoped; acquiring a lock on one pooled connection and
// running migrations through another would provide no protection.
type migrationExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

// loadMigrations reads embedded migration files, parses their numeric prefix
// as the version, and returns them sorted by version. Filenames must match
// "NNNN_description.sql" with a unique, contiguous, one-based version
// sequence; any violation is a build-time programming error, not a runtime
// condition to recover from.
func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("reading embedded migrations: %w", err)
	}

	migrations := make([]migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		versionPart, namePart, ok := strings.Cut(entry.Name(), "_")
		if !ok {
			return nil, fmt.Errorf("migration filename %q must be NNNN_description.sql", entry.Name())
		}
		version, err := strconv.Atoi(versionPart)
		if err != nil {
			return nil, fmt.Errorf("migration filename %q has non-numeric version: %w", entry.Name(), err)
		}
		data, err := migrationFS.ReadFile(path.Join("migrations", entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("reading migration %q: %w", entry.Name(), err)
		}
		migrations = append(migrations, migration{
			Version: version,
			Name:    strings.TrimSuffix(namePart, ".sql"),
			SQL:     string(data),
		})
	}

	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Version < migrations[j].Version })

	for i, m := range migrations {
		wantVersion := i + 1
		if m.Version != wantVersion {
			return nil, fmt.Errorf("migrations must be numbered contiguously from 1; found version %d at position %d", m.Version, wantVersion)
		}
	}

	return migrations, nil
}

// Migrate applies every pending migration inside its own transaction, in
// order, recording each as it commits. It refuses to proceed if the database
// already records a migration version newer than any known migration (a
// binary older than the schema it is pointed at) or a version gap (a
// partially applied migration history), rather than guessing a recovery
// path.
func (db *DB) Migrate(ctx context.Context) error {
	migrations, err := loadMigrations()
	if err != nil {
		return fmt.Errorf("sqlite: loading migrations: %w", err)
	}
	if db.postgres {
		return db.migratePostgres(ctx, migrations)
	}
	return db.migrateWith(ctx, db.sqlDB, migrations)
}

func (db *DB) migratePostgres(ctx context.Context, migrations []migration) error {
	conn, err := db.sqlDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("postgres: acquire migration connection: %w", err)
	}
	defer conn.Close() //nolint:errcheck // connection closure releases any remaining advisory lock

	// The numeric lock ID is a stable product-wide namespace, not a secret.
	// It serializes schema changes across all control-plane replicas sharing a
	// database without preventing ordinary application transactions.
	const migrationLockID int64 = 0x5a4f4e43504c414e
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, migrationLockID); err != nil {
		return fmt.Errorf("postgres: acquire migration advisory lock: %w", err)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(unlockCtx, `SELECT pg_advisory_unlock($1)`, migrationLockID)
	}()
	return db.migrateWith(ctx, conn, migrations)
}

func (db *DB) migrateWith(ctx context.Context, executor migrationExecutor, migrations []migration) error {

	if _, err := executor.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER PRIMARY KEY,
			name       TEXT NOT NULL,
			applied_at TEXT NOT NULL
		)`); err != nil {
		return fmt.Errorf("creating schema_migrations table: %w", err)
	}

	applied, err := db.appliedVersions(ctx, executor)
	if err != nil {
		return fmt.Errorf("sqlite: reading applied migrations: %w", err)
	}

	maxKnown := 0
	if len(migrations) > 0 {
		maxKnown = migrations[len(migrations)-1].Version
	}
	for _, v := range applied {
		if v > maxKnown {
			return fmt.Errorf("sqlite: database schema is at version %d, newer than the %d migrations known to this binary; refusing to start", v, maxKnown)
		}
	}
	for i := range applied {
		wantVersion := i + 1
		if applied[i] != wantVersion {
			return fmt.Errorf("sqlite: applied migration history has a gap at version %d; refusing to start against a partially applied schema", wantVersion)
		}
	}

	nextVersion := len(applied) + 1
	for _, m := range migrations {
		if m.Version < nextVersion {
			continue
		}
		if err := db.applyMigration(ctx, executor, m); err != nil {
			return fmt.Errorf("sqlite: applying migration %d_%s: %w", m.Version, m.Name, err)
		}
	}

	return nil
}

func (db *DB) appliedVersions(ctx context.Context, executor migrationExecutor) ([]int, error) {
	rows, err := executor.QueryContext(ctx, `SELECT version FROM schema_migrations ORDER BY version ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var versions []int
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		versions = append(versions, v)
	}
	return versions, rows.Err()
}

func (db *DB) applyMigration(ctx context.Context, executor migrationExecutor, m migration) error {
	tx, err := executor.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op

	sql := m.SQL
	if db.postgres {
		sql = postgresMigrationSQL(sql)
	}
	if _, err := tx.ExecContext(ctx, sql); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		db.placeholderSQL(`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`),
		m.Version, m.Name, time.Now().UTC().Format(time.RFC3339Nano),
	); err != nil {
		return err
	}
	return tx.Commit()
}

// postgresMigrationSQL converts the baseline schema's two SQLite-specific
// declarations to their PostgreSQL equivalents. Application timestamps remain
// RFC3339 text intentionally because the repository contract serializes and
// hashes them in that representation.
func postgresMigrationSQL(sql string) string {
	sql = strings.ReplaceAll(sql, "BLOB", "BYTEA")
	sql = strings.ReplaceAll(sql,
		"strftime('%Y-%m-%dT%H:%M:%fZ', 'now')",
		`to_char(CURRENT_TIMESTAMP AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')`,
	)
	return sql
}

func (db *DB) placeholderSQL(sql string) string {
	if !db.postgres {
		return sql
	}
	return postgresPlaceholders(sql)
}

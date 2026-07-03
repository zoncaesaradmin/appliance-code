package sqlite

import (
	"context"
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

	if _, err := db.sqlDB.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER PRIMARY KEY,
			name       TEXT NOT NULL,
			applied_at TEXT NOT NULL
		)`); err != nil {
		return fmt.Errorf("sqlite: creating schema_migrations table: %w", err)
	}

	applied, err := db.appliedVersions(ctx)
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
		if err := db.applyMigration(ctx, m); err != nil {
			return fmt.Errorf("sqlite: applying migration %d_%s: %w", m.Version, m.Name, err)
		}
	}

	return nil
}

func (db *DB) appliedVersions(ctx context.Context) ([]int, error) {
	rows, err := db.sqlDB.QueryContext(ctx, `SELECT version FROM schema_migrations ORDER BY version ASC`)
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

func (db *DB) applyMigration(ctx context.Context, m migration) error {
	tx, err := db.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op

	if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`,
		m.Version, m.Name, time.Now().UTC().Format(time.RFC3339Nano),
	); err != nil {
		return err
	}
	return tx.Commit()
}

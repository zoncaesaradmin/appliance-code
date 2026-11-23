package order

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
)

//go:embed schema.sql
var schemaSQL string

// EnsureSchema creates the ordering-owned tables in the appliance PostgreSQL
// database. The operation is idempotent and is intentionally limited to the
// ordering_* namespace; it never changes control-plane tables.
func EnsureSchema(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("ordering schema: database is nil")
	}
	if _, err := db.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("ordering schema: create tables: %w", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO ordering_schema_migrations(version) VALUES (1) ON CONFLICT (version) DO NOTHING`); err != nil {
		return fmt.Errorf("ordering schema: record version: %w", err)
	}
	return nil
}

package sqlite

import "testing"

func TestPostgresPlaceholders(t *testing.T) {
	got := postgresPlaceholders(`SELECT '?' AS literal, id FROM users WHERE id = ? AND username = ?`)
	const want = `SELECT '?' AS literal, id FROM users WHERE id = $1 AND username = $2`
	if got != want {
		t.Fatalf("postgresPlaceholders() = %q, want %q", got, want)
	}
}

func TestPostgresMigrationSQL(t *testing.T) {
	got := postgresMigrationSQL("digest BLOB, created_at TEXT DEFAULT strftime('%Y-%m-%dT%H:%M:%fZ', 'now')")
	if got == "" || got == "digest BLOB, created_at TEXT DEFAULT strftime('%Y-%m-%dT%H:%M:%fZ', 'now')" {
		t.Fatalf("postgresMigrationSQL did not translate PostgreSQL-incompatible SQL: %q", got)
	}
}

# ADR 0004: Control-Plane SQLite

- Status: Accepted
- Date: 2026-07-03

## Context

The first cut needs dependency-free local development and a small control-plane footprint. SQLite is appropriate when its single-writer and filesystem constraints are explicit.

## Decision

- Define domain-oriented repository interfaces and one SQLite adapter. Do not expose SQL driver types outside the adapter.
- Use the pure-Go `modernc.org/sqlite` driver unless the Phase 0 compatibility spike finds a blocking defect.
- Run exactly one control-plane replica while SQLite is active.
- Store the database on a dedicated ReadWriteOnce PVC backed by the appliance's local `ext4` data filesystem. NFS and other network filesystems are unsupported.
- Configure `foreign_keys=ON`, WAL journal mode, `synchronous=FULL`, a 5-second busy timeout, and a bounded connection pool with one writer. All values are asserted by startup tests rather than assumed from defaults.
- Keep the database directory mode `0700` and database/backup files `0600`.
- Use embedded, forward-only, versioned SQL migrations applied before readiness. Startup refuses unknown, dirty, partially applied, or newer schemas.
- Use the SQLite online backup API through the selected driver to create a transactionally consistent snapshot, followed by `integrity_check` on the backup before publication.
- Treat disk-full, I/O, corruption, and lock timeout as explicit domain/health failures. Never recreate or truncate the database automatically.

Local development and the first appliance release use this same adapter. Postgres is a later adapter plus a separately designed SQLite-to-Postgres data migration; interface compatibility alone is insufficient.

## Consequences

The control plane cannot scale horizontally in this phase. WAL, backup, restore, and volume behavior become mandatory release tests.

## Verification

- Repository contract suite and migration up/dirty/newer-schema tests
- Concurrent read/write and lock-timeout tests
- Disk-full, interrupted write, and corruption-detection tests
- Online backup and clean-instance restore tests
- Local Darwin and appliance Linux test lanes

## References

- [SQLite WAL](https://www.sqlite.org/wal.html)
- [SQLite online backup API](https://www.sqlite.org/backup.html)
- [SQLite PRAGMA reference](https://www.sqlite.org/pragma.html)


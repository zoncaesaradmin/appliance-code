-- Foundation tables shared across all future feature migrations: durable
-- async operations, idempotency records, and maintenance checkpoints. Entity
-- migrations for identity, RBAC, builds, artifacts, and audit land in later
-- Phase 2+ migrations.

CREATE TABLE operations (
    id            TEXT PRIMARY KEY,
    kind          TEXT NOT NULL,
    owner_id      TEXT,
    status        TEXT NOT NULL,
    result_body   BLOB,
    problem_body  BLOB,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

CREATE INDEX idx_operations_owner ON operations (owner_id);

CREATE TABLE idempotency_records (
    scope            TEXT NOT NULL,
    key              TEXT NOT NULL,
    request_hash     TEXT NOT NULL,
    response_status  INTEGER,
    response_body    BLOB,
    created_at       TEXT NOT NULL,
    expires_at       TEXT NOT NULL,
    PRIMARY KEY (scope, key)
);

CREATE INDEX idx_idempotency_expires ON idempotency_records (expires_at);

CREATE TABLE maintenance_checkpoints (
    task_name    TEXT PRIMARY KEY,
    last_run_at  TEXT,
    cursor       TEXT,
    updated_at   TEXT NOT NULL
);

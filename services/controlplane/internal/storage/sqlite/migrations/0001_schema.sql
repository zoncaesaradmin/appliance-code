-- Baseline pre-release SQLite schema for the appliance control plane.
--
-- This repo is still before its first real release, so we keep the schema as
-- one explicit SQL source of truth instead of preserving an incremental
-- migration history. When a real compatibility boundary exists, new schema
-- changes can resume as additive migrations from this baseline.

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

CREATE TABLE users (
    id                 TEXT PRIMARY KEY,
    username           TEXT NOT NULL UNIQUE,
    display_name       TEXT NOT NULL,
    state              TEXT NOT NULL DEFAULT 'active',
    credential_version INTEGER NOT NULL DEFAULT 1,
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL
);

CREATE TABLE password_credentials (
    user_id    TEXT PRIMARY KEY REFERENCES users (id),
    algorithm  TEXT NOT NULL,
    params     TEXT NOT NULL,
    salt       BLOB NOT NULL,
    hash       BLOB NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE password_reset_credentials (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users (id),
    lookup_id   TEXT NOT NULL UNIQUE,
    digest      BLOB NOT NULL,
    created_at  TEXT NOT NULL,
    expires_at  TEXT NOT NULL,
    used_at     TEXT
);

CREATE INDEX idx_password_reset_user ON password_reset_credentials (user_id);

CREATE TABLE permissions (
    name        TEXT PRIMARY KEY,
    description TEXT NOT NULL
);

CREATE TABLE roles (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    built_in   INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE role_permissions (
    role_id         TEXT NOT NULL REFERENCES roles (id),
    permission_name TEXT NOT NULL REFERENCES permissions (name),
    PRIMARY KEY (role_id, permission_name)
);

CREATE TABLE user_roles (
    user_id    TEXT NOT NULL REFERENCES users (id),
    role_id    TEXT NOT NULL REFERENCES roles (id),
    created_at TEXT NOT NULL,
    PRIMARY KEY (user_id, role_id)
);

CREATE TABLE api_tokens (
    id            TEXT PRIMARY KEY,
    user_id       TEXT NOT NULL REFERENCES users (id),
    name          TEXT NOT NULL,
    lookup_id     TEXT NOT NULL UNIQUE,
    digest        BLOB NOT NULL,
    scopes        TEXT,
    created_at    TEXT NOT NULL,
    expires_at    TEXT NOT NULL,
    last_used_at  TEXT,
    revoked_at    TEXT
);

CREATE INDEX idx_api_tokens_user ON api_tokens (user_id);

CREATE TABLE session_families (
    id                   TEXT PRIMARY KEY,
    user_id              TEXT NOT NULL REFERENCES users (id),
    created_at           TEXT NOT NULL,
    last_used_at         TEXT NOT NULL,
    absolute_expires_at  TEXT NOT NULL,
    revoked_at           TEXT,
    revoked_reason       TEXT
);

CREATE INDEX idx_session_families_user ON session_families (user_id);

CREATE TABLE refresh_credentials (
    family_id        TEXT PRIMARY KEY REFERENCES session_families (id),
    current_digest   BLOB NOT NULL,
    previous_digest  BLOB,
    version          INTEGER NOT NULL DEFAULT 1,
    expires_at       TEXT NOT NULL,
    rotated_at       TEXT NOT NULL
);

CREATE TABLE auth_throttle_state (
    username         TEXT PRIMARY KEY,
    failure_count    INTEGER NOT NULL DEFAULT 0,
    first_failure_at TEXT,
    last_failure_at  TEXT,
    locked_until     TEXT
);

CREATE TABLE audit_events (
    id             TEXT PRIMARY KEY,
    sequence       INTEGER NOT NULL,
    occurred_at    TEXT NOT NULL,
    actor_user_id  TEXT,
    actor_type     TEXT NOT NULL,
    auth_method    TEXT,
    credential_id  TEXT,
    action         TEXT NOT NULL,
    target_type    TEXT,
    target_id      TEXT,
    outcome        TEXT NOT NULL,
    reason_code    TEXT,
    request_id     TEXT,
    source_addr    TEXT,
    severity       TEXT NOT NULL DEFAULT 'info',
    details        TEXT,
    prev_hash      BLOB,
    hash           BLOB NOT NULL
);

CREATE UNIQUE INDEX idx_audit_events_sequence ON audit_events (sequence);
CREATE INDEX idx_audit_events_occurred ON audit_events (occurred_at);
CREATE INDEX idx_audit_events_actor ON audit_events (actor_user_id);

CREATE TABLE audit_checkpoints (
    id             TEXT PRIMARY KEY,
    created_at     TEXT NOT NULL,
    last_sequence  INTEGER NOT NULL,
    chain_hash     BLOB NOT NULL
);

CREATE TABLE registry_grants (
    id           TEXT PRIMARY KEY,
    subject_type TEXT NOT NULL,
    subject_id   TEXT NOT NULL,
    path_prefix  TEXT NOT NULL,
    actions      TEXT NOT NULL,
    created_at   TEXT NOT NULL
);

CREATE INDEX idx_registry_grants_subject ON registry_grants (subject_type, subject_id);

CREATE TABLE builds (
    id                   TEXT PRIMARY KEY,
    owner_id             TEXT NOT NULL,
    status               TEXT NOT NULL,
    source_repo_url      TEXT NOT NULL,
    source_commit_sha    TEXT NOT NULL,
    containerfile_path   TEXT NOT NULL DEFAULT 'Containerfile',
    image_repository     TEXT NOT NULL,
    image_tag            TEXT NOT NULL,
    builder_image_digest TEXT NOT NULL,
    workflow_name        TEXT,
    cancel_requested     INTEGER NOT NULL DEFAULT 0,
    reason_code          TEXT,
    error_message        TEXT,
    created_at           TEXT NOT NULL,
    updated_at           TEXT NOT NULL,
    started_at           TEXT,
    completed_at         TEXT,
    deadline_at          TEXT NOT NULL
);

CREATE INDEX idx_builds_owner ON builds (owner_id);
CREATE INDEX idx_builds_status ON builds (status);

CREATE TABLE workspaces (
    id                    TEXT PRIMARY KEY,
    owner_id              TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name                  TEXT NOT NULL,
    work_profile          TEXT NOT NULL,
    source_repo_url       TEXT NOT NULL,
    source_ref            TEXT NOT NULL,
    source_credential_ref TEXT,
    status                TEXT NOT NULL,
    reason_code           TEXT,
    error_message         TEXT,
    created_at            TEXT NOT NULL,
    updated_at            TEXT NOT NULL,
    deleted_at            TEXT,
    UNIQUE(owner_id, name)
);

CREATE INDEX idx_workspaces_owner ON workspaces(owner_id, created_at DESC);

CREATE TABLE current_workspaces (
    user_id      TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    updated_at   TEXT NOT NULL
);

CREATE TABLE jobs (
    id            TEXT PRIMARY KEY,
    owner_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    workspace_id  TEXT REFERENCES workspaces(id) ON DELETE SET NULL,
    build_id      TEXT REFERENCES builds(id) ON DELETE SET NULL,
    type          TEXT NOT NULL,
    status        TEXT NOT NULL,
    target_name   TEXT,
    artifact_ref  TEXT,
    reason_code   TEXT,
    error_message TEXT,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    started_at    TEXT,
    completed_at  TEXT
);

CREATE INDEX idx_jobs_owner ON jobs(owner_id, created_at DESC);
CREATE INDEX idx_jobs_build ON jobs(build_id);

CREATE TABLE job_steps (
    id           TEXT PRIMARY KEY,
    job_id       TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    status       TEXT NOT NULL,
    message      TEXT,
    created_at   TEXT NOT NULL,
    started_at   TEXT,
    completed_at TEXT
);

CREATE INDEX idx_job_steps_job ON job_steps(job_id, created_at ASC);

-- Identity, RBAC, session, and audit tables for Phase 2. Registry grants,
-- builds, and artifacts land in their own later migrations.

CREATE TABLE users (
    id                 TEXT PRIMARY KEY,
    username           TEXT NOT NULL UNIQUE,
    display_name       TEXT NOT NULL,
    state              TEXT NOT NULL DEFAULT 'active', -- 'active' | 'disabled'
    credential_version INTEGER NOT NULL DEFAULT 1,
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL
);

CREATE TABLE password_credentials (
    user_id    TEXT PRIMARY KEY REFERENCES users (id),
    algorithm  TEXT NOT NULL,
    params     TEXT NOT NULL, -- JSON-encoded Argon2idParams used for this hash
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
    scopes        TEXT, -- JSON array of permission names; NULL means "inherit all owner permissions"
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
    actor_type     TEXT NOT NULL, -- 'user' | 'api_token' | 'system' | 'anonymous'
    auth_method    TEXT,
    credential_id  TEXT,
    action         TEXT NOT NULL,
    target_type    TEXT,
    target_id      TEXT,
    outcome        TEXT NOT NULL, -- 'success' | 'failure' | 'denied'
    reason_code    TEXT,
    request_id     TEXT,
    source_addr    TEXT,
    severity       TEXT NOT NULL DEFAULT 'info', -- 'info' | 'high'
    details        TEXT, -- redacted JSON
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

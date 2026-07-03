-- Repository-prefix grants for OCI registry token authorization (ADR 0010
-- RBAC section). Built-in role implicit prefixes (administrator: **;
-- developer: ** pull plus users/<username>/** and builds/<username>/**
-- push; viewer: ** pull; automation: none) are computed at authorization
-- time and are not stored here; this table holds only explicit grants an
-- administrator assigns beyond those defaults.

CREATE TABLE registry_grants (
    id           TEXT PRIMARY KEY,
    subject_type TEXT NOT NULL, -- 'user' | 'role'
    subject_id   TEXT NOT NULL,
    path_prefix  TEXT NOT NULL, -- normalized lowercase slash-separated prefix
    actions      TEXT NOT NULL, -- JSON array subset of ["pull","push"]
    created_at   TEXT NOT NULL
);

CREATE INDEX idx_registry_grants_subject ON registry_grants (subject_type, subject_id);

-- Build requests and their durable state. Argo is operational state, not
-- durable product state: this table is the durable record of build intent,
-- ownership, source attribution, and outcome; workflow_name is only a
-- reference to reconcile against, never authoritative on its own.

CREATE TABLE builds (
    id                   TEXT PRIMARY KEY,
    owner_id             TEXT NOT NULL,
    status               TEXT NOT NULL, -- pending | running | succeeded | failed | cancelled | timed_out
    source_repo_url      TEXT NOT NULL, -- allowlisted internal HTTPS Git source
    source_commit_sha    TEXT NOT NULL, -- immutable full 40-hex commit SHA
    containerfile_path   TEXT NOT NULL DEFAULT 'Containerfile',
    image_repository     TEXT NOT NULL, -- normalized target OCI repository path
    image_tag            TEXT NOT NULL,
    builder_image_digest TEXT NOT NULL, -- pinned, approved builder image reference used
    workflow_name        TEXT,          -- set once submitted to the workflow engine
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

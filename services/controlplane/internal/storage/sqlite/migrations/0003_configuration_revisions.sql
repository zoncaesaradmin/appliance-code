-- Immutable, replayable desired-configuration revisions. This table does not
-- contain credentials, sessions, audit history, or a raw database backup.
CREATE TABLE configuration_revisions (
    id             TEXT PRIMARY KEY,
    schema_version TEXT NOT NULL,
    payload        BLOB NOT NULL,
    payload_digest TEXT NOT NULL,
    created_at     TEXT NOT NULL,
    created_by     TEXT,
    status         TEXT NOT NULL,
    apply_message  TEXT NOT NULL DEFAULT '',
    applied_at     TEXT
);

CREATE INDEX idx_configuration_revisions_created
    ON configuration_revisions (created_at DESC, id DESC);

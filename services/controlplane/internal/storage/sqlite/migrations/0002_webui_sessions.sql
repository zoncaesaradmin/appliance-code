CREATE TABLE webui_launch_grants (
    id         TEXT PRIMARY KEY,
    lookup_id  TEXT NOT NULL UNIQUE,
    digest     BLOB NOT NULL,
    user_id    TEXT NOT NULL REFERENCES users (id),
    family_id  TEXT NOT NULL REFERENCES session_families (id),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    used_at    TEXT
);
CREATE INDEX idx_webui_launch_grants_family ON webui_launch_grants (family_id);

CREATE TABLE webui_bridge_sessions (
    id         TEXT PRIMARY KEY,
    lookup_id  TEXT NOT NULL UNIQUE,
    digest     BLOB NOT NULL,
    user_id    TEXT NOT NULL REFERENCES users (id),
    family_id  TEXT NOT NULL REFERENCES session_families (id),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    revoked_at TEXT
);
CREATE INDEX idx_webui_bridge_sessions_family ON webui_bridge_sessions (family_id);

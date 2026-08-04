-- Licensing, custom appliance profiles, activation state, and acknowledgements.

CREATE TABLE licensing_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    state TEXT NOT NULL,
    license_document TEXT,
    license_summary_json TEXT NOT NULL DEFAULT '{}',
    accepted_at TEXT,
    accepted_by_user_id TEXT,
    updated_at TEXT NOT NULL
);

INSERT INTO licensing_state (id, state, updated_at)
VALUES (1, 'unresolved', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));

CREATE TABLE appliance_custom_profiles (
    id TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    capabilities_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    created_by_user_id TEXT
);

CREATE TABLE appliance_profile_activation (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    desired_profile_id TEXT,
    status TEXT NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL,
    updated_by_user_id TEXT
);

INSERT INTO appliance_profile_activation (id, desired_profile_id, status, message, updated_at)
VALUES (1, NULL, 'active', '', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));

CREATE TABLE notification_acknowledgements (
    notification_id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    acknowledged_at TEXT NOT NULL
);

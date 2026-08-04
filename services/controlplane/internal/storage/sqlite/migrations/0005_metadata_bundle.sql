-- Active and previous appliance metadata bundles.

CREATE TABLE metadata_bundle_state (
    slot TEXT PRIMARY KEY CHECK (slot IN ('active', 'previous')),
    metadata_version TEXT NOT NULL,
    software_version TEXT NOT NULL,
    digest TEXT NOT NULL DEFAULT '',
    directory_name TEXT NOT NULL,
    directory_path TEXT NOT NULL,
    signature TEXT NOT NULL DEFAULT '',
    installed_at TEXT NOT NULL,
    installed_by TEXT NOT NULL DEFAULT ''
);

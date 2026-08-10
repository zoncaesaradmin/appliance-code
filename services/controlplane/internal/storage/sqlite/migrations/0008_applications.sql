CREATE TABLE application_definitions (
    name TEXT NOT NULL,
    version TEXT NOT NULL,
    document BLOB NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (name, version)
);

CREATE TABLE application_instances (
    name TEXT PRIMARY KEY,
    definition_name TEXT NOT NULL,
    definition_version TEXT NOT NULL,
    desired_state TEXT NOT NULL,
    observed_state TEXT NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

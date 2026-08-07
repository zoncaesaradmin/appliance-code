-- Singleton runtime build catalog (operator-managed, not install-time Helm values).

CREATE TABLE builder_catalog (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    document_text TEXT NOT NULL DEFAULT '',
    content_type TEXT NOT NULL DEFAULT 'application/json',
    updated_at TEXT NOT NULL
);

INSERT INTO builder_catalog (id, document_text, content_type, updated_at)
VALUES (1, '', 'application/json', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));

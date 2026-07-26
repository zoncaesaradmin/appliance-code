CREATE TABLE dns_records (
    name              TEXT PRIMARY KEY,
    ipv4              TEXT NOT NULL,
    ttl               INTEGER NOT NULL,
    source            TEXT NOT NULL,
    owner             TEXT NOT NULL DEFAULT '',
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL,
    lease_expires_at  TEXT
);

CREATE INDEX idx_dns_records_lease ON dns_records (lease_expires_at);

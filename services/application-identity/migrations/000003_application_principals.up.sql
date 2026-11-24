-- Provider-neutral application identity context. Provider identifiers are
-- stored only as keyed digests in identity_channel_subjects.
CREATE TABLE identity_tenants (
    id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9.-]{1,62}$'),
    display_name text NOT NULL,
    status text NOT NULL CHECK (status IN ('active', 'suspended')),
    created_at timestamptz NOT NULL
);

INSERT INTO identity_tenants (id, display_name, status, created_at)
VALUES ('zonited-solutions', 'Zonited Solutions', 'active', CURRENT_TIMESTAMP)
ON CONFLICT (id) DO NOTHING;

CREATE TABLE identity_channels (
    id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9.-]{1,62}$'),
    tenant_id text NOT NULL REFERENCES identity_tenants (id),
    audience text NOT NULL CHECK (audience IN ('customer', 'vendor', 'zone-admin', 'tenant-admin')),
    auto_register boolean NOT NULL DEFAULT false,
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);
CREATE INDEX identity_channels_tenant_idx ON identity_channels (tenant_id);

INSERT INTO identity_channels (id, tenant_id, audience, auto_register, enabled, created_at, updated_at)
VALUES ('customer-web', 'zonited-solutions', 'customer', true, true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT (id) DO NOTHING;
INSERT INTO identity_channels (id, tenant_id, audience, auto_register, enabled, created_at, updated_at)
VALUES ('customer-whatsapp', 'zonited-solutions', 'customer', true, true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT (id) DO NOTHING;

CREATE TABLE identity_channel_subjects (
    id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{32}$'),
    channel_id text NOT NULL REFERENCES identity_channels (id),
    external_subject_key bytea NOT NULL CHECK (octet_length(external_subject_key) = 32),
    user_id text REFERENCES identity_users (id),
    status text NOT NULL CHECK (status IN ('pending', 'linked', 'blocked')),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (channel_id, external_subject_key)
);
CREATE INDEX identity_channel_subjects_user_idx ON identity_channel_subjects (user_id);

ALTER TABLE identity_sessions ADD COLUMN session_id text;
ALTER TABLE identity_sessions ADD COLUMN tenant_id text NOT NULL DEFAULT 'zonited-solutions' REFERENCES identity_tenants (id);
ALTER TABLE identity_sessions ADD COLUMN channel_id text NOT NULL DEFAULT 'customer-web' REFERENCES identity_channels (id);
ALTER TABLE identity_sessions ADD COLUMN subject_id text REFERENCES identity_channel_subjects (id);
ALTER TABLE identity_sessions ADD COLUMN audience text NOT NULL DEFAULT 'customer';
ALTER TABLE identity_sessions ADD COLUMN auth_method text NOT NULL DEFAULT 'mobile_otp';
CREATE UNIQUE INDEX identity_sessions_session_id_idx ON identity_sessions (session_id) WHERE session_id IS NOT NULL;
CREATE INDEX identity_sessions_subject_idx ON identity_sessions (subject_id);


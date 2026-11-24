CREATE TABLE identity_zones (
    id text PRIMARY KEY,
    tenant_id text NOT NULL REFERENCES identity_tenants (id),
    name text NOT NULL,
    latitude double precision NOT NULL CHECK (latitude >= -90 AND latitude <= 90),
    longitude double precision NOT NULL CHECK (longitude >= -180 AND longitude <= 180),
    join_radius_m integer NOT NULL DEFAULT 1000 CHECK (join_radius_m BETWEEN 1 AND 10000),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    created_at timestamptz NOT NULL
);
CREATE INDEX identity_zones_tenant_status_idx ON identity_zones (tenant_id, status);

CREATE TABLE identity_discovery_links (
    token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash) = 32),
    user_id text NOT NULL REFERENCES identity_users (id),
    tenant_id text NOT NULL REFERENCES identity_tenants (id),
    channel_id text NOT NULL REFERENCES identity_channels (id),
    audience text NOT NULL,
    expires_at timestamptz NOT NULL,
    used_at timestamptz,
    created_at timestamptz NOT NULL
);
CREATE INDEX identity_discovery_links_expiry_idx ON identity_discovery_links (expires_at);

CREATE TABLE identity_zone_join_requests (
    user_id text NOT NULL REFERENCES identity_users (id),
    tenant_id text NOT NULL REFERENCES identity_tenants (id),
    zone_id text NOT NULL REFERENCES identity_zones (id),
    status text NOT NULL CHECK (status IN ('pending', 'approved', 'rejected')),
    approval_mode text NOT NULL CHECK (approval_mode IN ('admin_review', 'auto_no_admin')),
    approved_at timestamptz,
    approved_by_user_id text REFERENCES identity_users (id),
    requested_at timestamptz NOT NULL,
    PRIMARY KEY (user_id, zone_id)
);
CREATE INDEX identity_zone_join_requests_tenant_status_idx ON identity_zone_join_requests (tenant_id, status);

CREATE TABLE identity_zone_admins (
    user_id text NOT NULL REFERENCES identity_users (id),
    tenant_id text NOT NULL REFERENCES identity_tenants (id),
    zone_id text NOT NULL REFERENCES identity_zones (id),
    status text NOT NULL CHECK (status IN ('active', 'revoked')),
    assigned_at timestamptz NOT NULL,
    PRIMARY KEY (user_id, zone_id)
);
CREATE INDEX identity_zone_admins_zone_status_idx ON identity_zone_admins (zone_id, status);

CREATE TABLE identity_zone_memberships (
    user_id text NOT NULL REFERENCES identity_users (id),
    tenant_id text NOT NULL REFERENCES identity_tenants (id),
    zone_id text NOT NULL REFERENCES identity_zones (id),
    status text NOT NULL CHECK (status IN ('active', 'revoked')),
    joined_at timestamptz NOT NULL,
    PRIMARY KEY (user_id, zone_id)
);
CREATE INDEX identity_zone_memberships_zone_status_idx ON identity_zone_memberships (zone_id, status);

CREATE TABLE identity_zone_creation_requests (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id text NOT NULL REFERENCES identity_users (id),
    tenant_id text NOT NULL REFERENCES identity_tenants (id),
    requested_name text NOT NULL CHECK (char_length(requested_name) BETWEEN 2 AND 100),
    status text NOT NULL CHECK (status IN ('pending', 'accepted', 'rejected')),
    requested_at timestamptz NOT NULL
);
CREATE UNIQUE INDEX identity_zone_creation_requests_user_name_idx ON identity_zone_creation_requests (user_id, requested_name);
CREATE INDEX identity_zone_creation_requests_tenant_status_idx ON identity_zone_creation_requests (tenant_id, status);

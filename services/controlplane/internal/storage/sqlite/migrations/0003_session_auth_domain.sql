-- Authentication domain for interactive session families.
-- V1 stores only "local"; future IdPs (ldap/oidc/...) reuse this column
-- so refresh and access-token reissue do not lose login routing context.
ALTER TABLE session_families ADD COLUMN auth_domain TEXT NOT NULL DEFAULT 'local';

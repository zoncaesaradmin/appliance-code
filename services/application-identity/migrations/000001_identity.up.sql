-- Identity-only schema for the appliance application sign-in service.
CREATE TABLE identity_users (
    id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{32}$'),
    phone text NOT NULL UNIQUE CHECK (phone ~ '^\+[1-9][0-9]{7,14}$'),
    onboarding_state text NOT NULL DEFAULT 'identified' CHECK (onboarding_state IN ('identified', 'join_pending', 'onboarded', 'suspended')),
    created_at timestamptz NOT NULL
);

CREATE TABLE identity_otp_challenges (
    id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{32}$'),
    phone text NOT NULL CHECK (phone ~ '^\+[1-9][0-9]{7,14}$'),
    digest bytea NOT NULL CHECK (octet_length(digest) = 32),
    expires_at timestamptz NOT NULL,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    consumed_at timestamptz
);
CREATE INDEX identity_otp_challenges_expiry_idx ON identity_otp_challenges (expires_at);

CREATE TABLE identity_otp_issuances (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    phone_key bytea NOT NULL CHECK (octet_length(phone_key) = 32),
    client_key bytea NOT NULL CHECK (octet_length(client_key) = 32),
    created_at timestamptz NOT NULL
);
CREATE INDEX identity_otp_issuances_phone_time_idx ON identity_otp_issuances (phone_key, created_at);
CREATE INDEX identity_otp_issuances_client_time_idx ON identity_otp_issuances (client_key, created_at);

CREATE TABLE identity_sessions (
    token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash) = 32),
    user_id text NOT NULL REFERENCES identity_users (id),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    CHECK (expires_at > created_at)
);
CREATE INDEX identity_sessions_user_idx ON identity_sessions (user_id);
CREATE INDEX identity_sessions_expiry_idx ON identity_sessions (expires_at);


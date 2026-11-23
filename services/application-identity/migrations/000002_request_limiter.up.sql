CREATE TABLE identity_request_attempts (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    bucket_key bytea NOT NULL CHECK (octet_length(bucket_key) = 32),
    created_at timestamptz NOT NULL
);
CREATE INDEX identity_request_attempts_bucket_time_idx ON identity_request_attempts (bucket_key, created_at);
CREATE INDEX identity_request_attempts_time_idx ON identity_request_attempts (created_at);
GRANT INSERT, DELETE ON TABLE identity_request_attempts TO shared_runtime;

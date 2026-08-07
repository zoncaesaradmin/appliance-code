-- Build-scoped registry push credential bookkeeping.
-- PushTokenID references the temporary API token minted for the build pod;
-- RegistrySecretName is the Kubernetes Secret that carries username/token.

ALTER TABLE builds ADD COLUMN push_token_id TEXT;
ALTER TABLE builds ADD COLUMN registry_secret_name TEXT;

# ADR 0005: Secrets, Keys, And TLS

- Status: Accepted
- Date: 2026-07-03

## Context

Session signing, credential hashing, registry-token signing, cursor integrity, audit checkpoints, TLS, backup encryption, and bootstrap need independent lifecycles. Storing all secrets in application configuration or reusing one key would make rotation and recovery unsafe.

## Decision

- The installer generates independent secrets from the operating-system CSPRNG for session signing, registry-token signing, API-token digesting, refresh/reset-credential digesting, cursor integrity, audit-checkpoint signing, bootstrap, backup encryption, and TLS.
- Session JWTs use Ed25519 with `kid`, configured issuer, explicit audience, 15-minute access lifetime, and at most 60 seconds of clock skew. Rotation keeps the previous public key valid only for the maximum access-token lifetime.
- Refresh and password-reset credentials are opaque 256-bit random values stored only as keyed digests under an ephemeral-credential pepper separate from the API-token pepper. Refresh credentials rotate on every use and are revoked on reuse, logout, password reset, or user disable.
- API tokens are opaque 256-bit random values with a public lookup ID and a keyed HMAC-SHA-256 digest using a dedicated pepper. Raw refresh/API secrets are returned once and never logged.
- Local passwords use Argon2id parameters calibrated on the minimum supported host and recorded with each hash. Password policy favors length, blocks known/default credentials, and does not require periodic rotation without evidence of compromise.
- Secrets live in purpose-specific Kubernetes Secrets mounted as files, not command-line arguments or general environment dumps. Enable K3s secret encryption at rest and tightly restrict Secret RBAC.
- The encrypted backup includes the key material required to validate sessions and API-token digests. Backup encryption keys are also exported through a separately protected recovery procedure so the backup is not self-locking.
- TLS supports two production modes: operator-supplied certificate/key and installer-generated appliance CA/leaf certificate. Local development may use loopback HTTP. ACME is deferred.
- Changing hostname requires certificate reissue and canonical-URL validation. Certificate expiry warnings begin at 30 days and become readiness/operator errors at expiry without causing destructive restart loops.
- Registry-token signing keys rotate independently from session keys and API-token digest peppers.
- Cursor HMAC keys rotate with a 24-hour verification overlap. Audit checkpoints use a dedicated Ed25519 signing key and include `kid`; neither key is reused for sessions, registry tokens, or release signing.

## Consequences

Losing the token-digest pepper makes existing API tokens unverifiable. Losing signing keys invalidates sessions. Those are acceptable only when the operator intentionally chooses a credential-reset recovery; normal restore therefore includes protected key material.

## Verification

- Key generation permission and redaction tests
- Normal and emergency rotation tests
- Restore with keys and intentional restore-without-keys behavior
- Expired/not-yet-valid/wrong-audience/wrong-issuer JWT tests
- TLS replacement, hostname change, and expiry-alert tests

## References

- [K3s secrets encryption](https://docs.k3s.io/security/secrets-encryption)
- [OWASP Password Storage Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html)

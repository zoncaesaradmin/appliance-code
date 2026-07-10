# ADR 0010: V1 Security And Operations Defaults

- Status: Accepted with validation gates
- Date: 2026-07-03

## Context

The architecture is settled, but implementation still needs concrete defaults for sessions, API tokens, MCP authorization, RBAC, listeners, configuration, audit retention, telemetry, vulnerability policy, signing, and support lifetime. Leaving these choices to individual handlers or packaging scripts would create inconsistent security behavior.

## Decision

### External Origin And Listeners

- Production has one required canonical origin: `https://<appliance-fqdn>`.
- Traefik exposes only TCP 443. An optional TCP 80 listener may perform an unconditional redirect to the canonical HTTPS origin; it serves no application content.
- `/api/v1/*`, `/mcp`, and `/v2/*` share the canonical origin. This avoids cross-origin credential and certificate complexity.
- The control plane has a public application listener and a separate internal operations listener. Health, metrics, profiling, and detailed dependency status are never routed through public ingress.
- TLS terminates at Traefik in v1. Cluster-internal HTTP is protected by namespace isolation and default-deny NetworkPolicy. Internal mTLS is deferred until a demonstrated threat or multi-node topology justifies its certificate lifecycle.
- HSTS is enabled after successful TLS provisioning with `max-age=31536000`; do not use `includeSubDomains` or preload in v1.
- Production requires a DNS name and matching certificate. Direct IP origins are supported only for explicit local/test mode.
- V1 production support is IPv4-only. IPv6 must not be silently accepted as supported; dual-stack becomes a separately tested compatibility profile.
- Working DNS and synchronized time are installation prerequisites. The installer fails with actionable diagnostics when resolution or clock skew is unsafe.

### Configuration

- Precedence is command-line operational flags, environment variables, configuration file, then compiled defaults.
- Secrets are accepted only through mounted files, file descriptors, or interactive bootstrap input, never ordinary flags or general environment variables.
- Unknown configuration keys and invalid combinations fail startup. The effective non-secret configuration and its schema version are available to authorized operators.
- V1 does not require or configure a public outbound proxy. Cluster service ranges, loopback, the canonical appliance host, and internal service names remain local; any future proxy support requires explicit allowlists and offline-invariant tests.

### Interactive Sessions

- Session access JWT lifetime is 15 minutes with at most 60 seconds clock skew.
- The opaque refresh credential has a 12-hour idle lifetime and a seven-day absolute session-family lifetime.
- Refresh credentials rotate on every use. Reuse revokes the complete session family and produces a high-severity audit event.
- Each user may have at most five concurrent interactive session families. Creating another revokes the least recently used family after explicit confirmation by the client.
- Logout revokes the current family. Password reset, credential-version change, or user disable revokes every session immediately.
- Session signing uses Ed25519 with issuer, audience, `kid`, `jti`, `iat`, `nbf`, and `exp`. Planned key rotation overlaps for one access-token lifetime.

### API Tokens

- API tokens are opaque 256-bit values, shown once, stored as keyed digests, and prefixed with a non-secret lookup identifier.
- Default lifetime is 90 days; maximum lifetime is 365 days; minimum lifetime is one hour. V1 does not issue non-expiring tokens.
- Tokens inherit the current permissions of their owner and may optionally reduce, never expand, those permissions through an allowed scope subset.
- User disable, token revocation, or credential-version change is effective immediately at the control plane and prevents new zot access tokens.
- `last_used_at` may be batched asynchronously but must become durable within five minutes. Authentication does not fail merely because this advisory update fails.
- Raw tokens are never accepted in query parameters and never appear in command arguments, logs, metrics, events, support bundles, or audit details.

### Password And Abuse Policy

- Login usernames are canonical lowercase ASCII, 3 to 64 characters, matching `[a-z][a-z0-9._-]*`. Unicode belongs in a separate display-name field and is never an authentication identifier.
- Login usernames are immutable after creation because they anchor personal zot namespaces. A mutable Unicode display name is presentation-only.
- Passwords are exact valid UTF-8 input with 8 to 128 Unicode code points and at most 1024 bytes. Do not normalize or trim them; canonically equivalent strings remain distinct. Passphrases and password-manager output are allowed; composition rules and periodic forced rotation are not used.
- Reject known breached, default, username-derived, or product-derived passwords. Calibrate Argon2id on the minimum supported appliance and store parameters with each hash.
- Login and recovery responses do not reveal whether an account exists.
- Apply rate limits by normalized account and trusted source. Durable account counters live in SQLite; bounded source-address token buckets may remain in memory and reset on restart. After five failures in 15 minutes, add progressive delay; after 20 failures in one hour, deny login for 15 minutes. Successful login clears the account failure counter.
- Node-local break-glass can clear a lock and reset a password. Every recovery action is auditable.
- Remote administrator-assisted reset issues one opaque 256-bit reset credential, returns it once, stores only a keyed digest, expires it after 15 minutes, and allows one use. Successful reset revokes all sessions and API tokens for that user. Administrators never choose or view the user's new password.

### MCP Authorization

- V1 uses the appliance API token in `Authorization: Bearer` as an explicitly documented MCP compatibility mode.
- V1 does not claim conformance with the MCP OAuth authorization profile and does not publish OAuth protected-resource metadata.
- The MCP endpoint still implements the pinned Streamable HTTP protocol, validates `Origin` when present, uses the same RBAC engine as REST, and returns an empty tool list until modules are enabled.
- Add standards-mode MCP authorization later through a separate OAuth resource-server adapter backed by an external or appliance-approved authorization server. API tokens and OAuth access tokens remain distinct credential classes.

### RBAC And Ownership

V1 uses appliance-wide roles, ownership checks, and simple zot repository-prefix grants. A full project model is deferred, but repository authorization is not appliance-wide for mutation.

- Normalize repository names to lowercase slash-separated paths before policy evaluation and reject ambiguous, traversal, empty, reserved, or non-canonical segments.
- Store grants as subject user/role, normalized path prefix, and allowed `pull`/`push` actions. Evaluate each requested action independently and allow it when at least one matching user/role grant explicitly permits that action; deny is the default and anonymous access is always denied. V1 has no explicit deny grants.
- `administrator` receives `pull`/`push` over `**` and may authorize control-plane deletion.
- `developer` receives `pull` over `**` plus `push` over `users/<username>/**` and `builds/<username>/**`.
- `viewer` receives `pull` over `**` only.
- `automation` receives no implicit push namespace. An administrator must assign explicit source/destination prefixes before creating its API token.
- The registry token issuer intersects role permission, prefix grant, requested repository/action, account state, and optional API-token scope. It never signs a broader claim.

Built-in roles are immutable:

| Role | Effective v1 access |
| --- | --- |
| `administrator` | Every published API permission, system operations, grant administration, audit access, and all-resource access; node-only recovery remains outside API RBAC |
| `developer` | Own token lifecycle; create/read/cancel own builds; read artifacts; delete artifacts produced by own builds; registry pull globally and push to personal/build prefixes; MCP invoke |
| `viewer` | Own token lifecycle; read all builds and artifacts; registry pull globally; no mutation or MCP invoke |
| `automation` | No interactive login; administrator-created API tokens and explicit repository-prefix grants; create/read/cancel own builds; read artifacts; no user/role administration |

Use explicit permissions rather than broad `write` aliases:

- `users.read`, `users.create`, `users.update`, `users.disable`
- `roles.read`, `roles.create`, `roles.update`, `roles.delete`
- `tokens.read.self`, `tokens.create.self`, `tokens.revoke.self`, `tokens.revoke.any`
- `tokens.create.any`
- `builds.create`, `builds.read.self`, `builds.read.any`, `builds.cancel.self`, `builds.cancel.any`
- `artifacts.read`, `artifacts.delete.self`, `artifacts.delete.any`
- `operations.read.self`, `operations.read.any`
- `registry.pull`, `registry.push`, `registry.delete`
- `registry.grants.read`, `registry.grants.write`
- `mcp.invoke`
- `system.read`, `system.operate`, `audit.read`, `audit.export`

Custom roles may combine published permissions. `administrator` is not represented by a bypass permission: normal authorization still evaluates explicit effective permissions, and the last-effective-administrator invariant always applies.

### API And HTTP Defaults

- REST errors use RFC 9457 `application/problem+json`; resource IDs are UUIDv7; timestamps are RFC 3339 UTC.
- List endpoints default to 50 entries and cap at 200. Cursors are opaque, integrity-protected, query-bound, and expire after 24 hours.
- JSON bodies default to a 1 MiB limit; endpoint-specific lower limits are preferred. OCI payloads never traverse the control plane.
- Ingress and server limits start at: 16 KiB total request headers, 5-second read-header timeout, 30-second ordinary request timeout, 60-second idle timeout, and 30-second graceful drain. Streaming endpoints define separate bounded policies.
- Create operations with side effects require an idempotency key retained for 24 hours. Mutable resources use strong ETags and `If-Match`; stale writes return `412`.
- CORS is deny-by-default. Credential responses use `Cache-Control: no-store`; public responses use strict content types, `nosniff`, and frame denial.

### Audit

- Authentication success, security mutations, and their audit records fail closed if the durable audit write cannot commit. Authentication denials remain denied and fall back to an emergency bounded node-local security log if SQLite is unavailable.
- Authentication failures and non-mutating access events may use a bounded asynchronous queue, but queue overflow is surfaced as degraded health and a high-severity local log event.
- Audit retention defaults to 365 days and is configurable from 90 to 3650 days. Storage pressure never silently shortens configured retention.
- Audit records are append-only through the application, hash-chained in daily segments, and checkpointed into off-appliance backup/export. This is tamper-evident, not tamper-proof against host root.
- Operator export uses versioned JSON Lines with schema version, checkpoint, and verification metadata.

### Background Work

- The single-replica control plane runs token/session cleanup, audit checkpoint/export scheduling, build-status reconciliation, and zot catalog reconciliation through one in-process maintenance manager with durable checkpoints, bounded batches, leases, jitter, and cancellation.
- Jobs are idempotent and safe to resume after restart. Failure degrades the affected subsystem and alerts operators without creating duplicate mutations.
- Coordinated backup remains a separate appliance-owned Kubernetes CronJob or node-level installer operation so recovery does not depend on a healthy application process.

### Telemetry, Vulnerability, And Supply Chain

- Outbound product telemetry is off by default. There is no silent phone-home behavior.
- Local Prometheus-format metrics are enabled only on the internal listener. Support-bundle creation and upload are explicit operator actions.
- Use pinned Syft for SBOM generation and pinned Grype for vulnerability scanning. Produce CycloneDX JSON and SPDX JSON for release components. Air-gapped systems import a signed, versioned Grype database bundle.
- Do not use mutable setup/actions or floating scanner images. Verify upstream checksums/signatures/attestations, mirror approved binaries/images internally by digest, and record scanner plus database identity in every result.
- Trivy is not a v1 release dependency because its March 2026 release ecosystem compromise is too recent for it to be the sole appliance gate. Reconsidering it requires a scanner ADR and independent provenance controls.
- Disable zot CVE refresh and every other background internet updater. The appliance release/import workflow owns updates.
- A release is blocked by any known Critical vulnerability. High vulnerabilities require a documented, signed, time-limited exception of at most 30 days with exploitability analysis and mitigation. Medium and lower findings are tracked and published according to policy.
- Use Skopeo with Sigstore-compatible key-based signing for promoted OCI images and policy verification. Use a pinned Cosign release tool only for signing/verifying release manifests, provenance, and non-image blobs; Cosign is not an appliance runtime dependency.
- Production images and charts are referenced by digest. Generate in-toto/SLSA provenance statements for release artifacts and build outputs where inputs are known.

### Support And Upgrade Policy

- Support the current appliance release and its immediate predecessor for upgrade. Each release receives security fixes for 12 months unless a longer commercial policy supersedes this baseline.
- Target remediation after triage: seven days for Critical and 30 days for High severity when an upstream fix or viable mitigation exists.
- Only N-1 to N in-place upgrade is supported. Older versions restore through documented intermediate upgrades or clean-node migration.
- Rollback is restore-based. Schema migrations are forward-only, transactional where possible, and preceded by an automatically verified backup.

### zot Profile

- Start with the pinned full zot image because enhanced search is a product requirement, but explicitly disable every extension not listed here.
- Enable enhanced search without CVE refresh, periodic scrub, internal metrics, and internal event delivery to the control plane.
- Enable filesystem `commit`, ext4 hard-link deduplication, and garbage collection with a 24-hour delay only after ADR 0008 storage tests pass.
- Disable UI, management mutation APIs, user preferences, sync/mirroring, profiling, lint, image trust, and CVE scanning in the first release. UI remains gated until browser authentication can reuse appliance identity without a second session authority and repository filtering is proven.
- Treat zot events as hints and rebuild all derived search/catalog state from authoritative OCI content after restore.

## Validation Gates

- Phase 0 may adjust numeric limits only with measured evidence on the minimum supported appliance. Any security relaxation requires a superseding ADR.
- Exact component versions and digests remain release compatibility data and must pass the Phase 0 matrix.
- Rate-limit, session, RBAC, audit-retention, telemetry, vulnerability exception, signing, air-gap, and support-policy tests are release gates.

## References

- [MCP HTTP authorization](https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization)
- [OAuth 2.0 Security Best Current Practice, RFC 9700](https://www.rfc-editor.org/rfc/rfc9700)
- [OWASP Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)
- [RFC 9457 Problem Details](https://www.rfc-editor.org/rfc/rfc9457)
- [Skopeo](https://github.com/containers/skopeo)
- [Syft](https://github.com/anchore/syft)
- [Grype](https://github.com/anchore/grype)
- [Trivy March 2026 security advisory](https://github.com/aquasecurity/trivy/security/advisories/GHSA-69fq-xp46-6x23)
- [Sigstore Cosign](https://github.com/sigstore/cosign)

# ADR 0002: Nexus Identity And Storage

- Status: Superseded by [ADR 0007](0007-cncf-distribution-registry.md)
- Date: 2026-07-03

This decision was superseded because Nexus does not expose a supported external OCI registry token-issuer contract, introduces edition/licensing concerns, and requires additional database and identity projection machinery that conflicts with the appliance control-plane architecture.

## Context

Nexus owns its registry bearer-token realm. The control plane cannot assume that Nexus accepts an independently signed registry token. Nexus user tokens are a Pro feature, while the first release should not require a commercial license. Current Sonatype guidance also does not support container deployments using the embedded H2 database.

## Decision

Use a pinned, currently supported Nexus Repository Community Edition release with a dedicated single-replica PostgreSQL service pod and a filesystem blob-store PVC.

The PostgreSQL service is initially for Nexus only. The appliance control plane continues to use SQLite. A future control-plane Postgres adapter must use a separate database, schema, role, and migration lifecycle.

The Nexus database role owns only its database/schema and the required `pg_trgm` extension. PostgreSQL and blob-store volumes are separate RWO PVCs, are not exposed outside the appliance namespaces, and are included in coordinated backup and capacity monitoring.

Registry identity uses token shadow principals:

- Each appliance API token that is allowed registry access maps to one disabled-by-default Nexus local user named from the immutable token ID, for example `apt_<token-id>`.
- During token creation, the control plane generates one 256-bit API-token secret and sends that same raw value once as the shadow principal password while assigning reduced Nexus roles. The control plane persists only its keyed digest.
- The token-creation response returns the API token once plus the registry username and registry URL. Podman uses the shadow username and the same API token as its password.
- The control plane never provisions human interactive passwords into Nexus.
- Built-in projection is appliance-wide for v1: viewer gets pull; developer and automation get pull/push; administrator gets repository administration through the control plane. Direct Nexus administration is not granted to ordinary appliance users.
- Nexus's registry bearer-token realm remains the issuer for OCI client bearer tokens. `/v2/*` routes to Nexus. The control plane does not publish a speculative registry token issuer.
- `/api/v1/registry/login-info` returns authenticated connection metadata and projection status but no secret. The earlier speculative `/api/v1/registry/auth` contract is removed unless a future integration requires it.
- Token creation is a saga: create a `provisioning` token record, create the Nexus shadow principal, mark the token active, then return the secret. A failed projection returns no token and is cleaned up.
- Revocation marks the appliance token revoked first, then synchronously disables the Nexus principal. Failed projection creates a high-severity degraded state and is retried by reconciliation. Healthy-system convergence target is 60 seconds; effective bearer-token revocation is bounded by the measured Nexus token lifetime.
- Build jobs never receive the requester's API token. The adapter creates a separate `bld_<build-id>` principal with a random credential and push access only for that build, then disables it when the build reaches a terminal state.
- Nexus management APIs are reachable only from the control plane and operator diagnostics network paths. Product ingress exposes repository content routes, not Nexus UI or administration.
- Configure Nexus password hashing to a supported PBKDF2-HMAC-SHA256 setting and keep Groovy scripting disabled.

Repository scope is deliberately simple in v1: one hosted OCI repository with appliance-wide pull/push roles. Project/repository namespace authorization is deferred until the product has a project domain model.

## Consequences

This preserves one automation secret without building a registry gateway or requiring Nexus Pro. It introduces shadow-principal reconciliation and means registry revocation cannot be stronger than Nexus bearer-token expiry during a partial failure.

The exact Nexus version, APIs, username limits, password handling, permission-cache behavior, and bearer-token lifetime must be proven before implementation. Failure of that spike reopens this ADR; it does not justify silently switching to stored plaintext credentials.

## Verification

- Provision two independent API tokens for one user and use both concurrently
- Podman login, pull, push, denied push, revoke, user disable, and role-change tests
- Nexus restart and reconciliation tests
- One-way dependency failure and revocation-latency tests
- PostgreSQL and blob-store coordinated backup/restore test

## References

- [Nexus documentation](https://help.sonatype.com/en/)
- [Nexus security management API](https://help.sonatype.com/en/security-management-api.html)
- [Nexus system requirements and database constraints](https://help.sonatype.com/en/sonatype-nexus-repository-system-requirements.html)
- [Install Nexus with PostgreSQL](https://help.sonatype.com/en/install-nexus-repository-with-a-postgresql-database.html)

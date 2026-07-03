# ADR 0007: CNCF Distribution Registry

- Status: Superseded by [ADR 0008](0008-zot-oci-artifact-registry.md)
- Date: 2026-07-03
- Supersedes: [ADR 0002](0002-nexus-identity-and-storage.md)

## Context

The appliance control plane must remain the authority for users, API tokens, RBAC, and revocation. The registry must challenge OCI clients using a control-plane token-service URL and verify short-lived, repository-scoped tokens signed by the control plane.

Nexus owns its registry bearer-token realm and does not provide a supported configuration for trusting an independent OCI token issuer. Harbor owns another identity/RBAC and token-service stack and brings PostgreSQL and Redis. Pulp supports multiple package formats and external token authentication but brings a larger Python/Django/PostgreSQL service stack. Zot is a strong Apache-2.0, single-binary OCI alternative with external bearer verification and useful built-in features, but CNCF Distribution is the smaller, more established data plane and exposes the exact external token configuration required here.

## Decision

Use a pinned CNCF Distribution v3 release as the v1 OCI registry data plane.

- License baseline is Apache-2.0. Record the exact image, source release, transitive notices, checksums, SBOM, and license text in every appliance release.
- Run one unprivileged registry pod with a dedicated ReadWriteOnce filesystem PVC on the appliance data volume. No registry database, Redis, or separate identity store is introduced.
- Configure token authentication with the control-plane realm, stable service/audience, issuer, EdDSA signing algorithm, and a mounted public-key/JWKS trust bundle.
- Expose `GET /api/v1/registry/token` as the OCI registry token-service endpoint. It accepts the standard `service`, repeated `scope`, `account`, `offline_token`, and client parameters required by supported clients.
- `podman login`, `skopeo login`, and `buildah login` use the appliance username as the Basic username and an appliance API token as the Basic password. The endpoint verifies that the username/account matches the token owner. Bearer input is supported for direct API compatibility. Interactive passwords are never accepted.
- V1 does not issue registry refresh tokens. An `offline_token` request may be accepted for client compatibility, but the response still contains only a short-lived access token.
- The control plane parses the requested OCI scope, intersects it with current appliance RBAC, and signs only the standard allowed `repository:<name>:pull,push` actions. It never reflects unvalidated scope strings.
- Registry access JWTs use a registry-specific Ed25519 key, issuer, audience/service, `kid`, subject, issued-at/not-before, unique token ID, and a five-minute maximum lifetime. Registry keys are not session-signing keys.
- API-token revocation or user disable immediately prevents new registry tokens. Existing registry access remains bounded by the five-minute token lifetime. Emergency registry-key rotation may invalidate all outstanding registry tokens.
- `/v2/*` routes directly to Distribution. The control plane is not in the image-layer data path.
- Start with one private hosted OCI namespace model. Repository names are normalized and authorized by path prefix. Anonymous access is disabled.
- Build jobs receive a build-specific, short-lived appliance credential and never the requester's API token.
- Enable manifest delete only through control-plane-authorized workflows using the exact registry action required by the pinned implementation. Run garbage collection in an explicit read-only maintenance window until the pinned Distribution release proves a safe online mode for our storage configuration.
- Use the registry notifications API only as a hint. Reconcile artifact metadata from the registry API/storage state so dropped or duplicated notifications cannot corrupt the control-plane source of truth.
- The control plane stores appliance-specific artifact metadata; Distribution remains authoritative for manifests, tags, digests, and blobs.

## Product Boundary

V1 supports OCI images and OCI artifacts only. It does not claim Maven, npm, PyPI, NuGet, or generic package-repository compatibility.

Additional package formats must be separate feature modules with their own data-plane adapters. They must reuse the same appliance identity and authorization services and must not force the OCI registry to become a general package manager.

## Alternatives

The detailed product and licensing comparison is in [Registry Options Review](../registry-options.md).

- Managed `htpasswd`: rejected because it cannot provide the required repository-action RBAC model and requires credential-file reconciliation.
- Harbor: rejected for v1 because it duplicates users, projects, RBAC, token service, UI, PostgreSQL, and Redis that the appliance control plane is intended to own.
- Pulp: deferred for a future multi-format package module; it supports external registry token authentication but has a materially larger operational footprint.
- Zot: retained as the preferred fallback if Distribution v3 fails the conformance or operational gate. It is Apache-2.0, OCI-native, single-binary, and supports external bearer-token verification.
- Custom registry or Nexus plugin/gateway: rejected because it creates unnecessary security-critical code and upgrade coupling.

## Consequences

The registry deployment and backup model becomes much smaller, and registry authorization exactly matches appliance RBAC. The control plane must implement the OCI registry token endpoint correctly and must own repository policy, artifact presentation, quotas, and lifecycle orchestration.

Distribution does not provide the broad package-format support, UI, scanning workflow, or policy suite of larger repository managers. Those are deliberate feature-module decisions rather than hidden dependencies in v1.

## Conformance Gate

- Pin the Distribution image by digest and verify its Apache-2.0 notices and release provenance.
- Test Podman, Skopeo, and Buildah login, pull, push, inspect/copy, manifest/tag listing, delete, denied scope, malformed scope, token expiry, user disable, and API-token revocation.
- Verify issuer, audience/service, subject, `kid`, signature, `nbf`, `exp`, and repository/action enforcement with positive and negative token fixtures.
- Verify key overlap and emergency rotation behavior.
- Test restart, interrupted upload cleanup, disk-full behavior, storage integrity, read-only maintenance, garbage collection dry-run/live operation, and clean-node restore.
- Run the OCI Distribution conformance suite applicable to the pinned release.

## References

- [CNCF Distribution configuration](https://distribution.github.io/distribution/about/configuration/)
- [Distribution registry token authentication](https://distribution.github.io/distribution/spec/auth/)
- [Distribution garbage collection](https://distribution.github.io/distribution/about/garbage-collection/)
- [OCI Distribution Specification](https://github.com/opencontainers/distribution-spec)
- [Distribution source and Apache-2.0 license](https://github.com/distribution/distribution)
- [zot authentication and authorization](https://zotregistry.dev/v2.1.15/articles/authn-authz/)

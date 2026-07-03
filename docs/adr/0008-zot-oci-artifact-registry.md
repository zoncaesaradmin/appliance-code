# ADR 0008: zot OCI Artifact Registry

- Status: Accepted with conformance gate
- Date: 2026-07-03
- Supersedes: [ADR 0007](0007-cncf-distribution-registry.md)

## Context

The appliance needs a lightweight, container-native artifact data plane that can run as one unprivileged pod without a separate database or identity service. The appliance control plane must remain authoritative for users, API tokens, RBAC, revocation, repository policy, and audit.

V1 must support Buildah, Podman, and Skopeo image workflows plus generic OCI artifacts through ORAS. It must not imply native npm, Maven, PyPI, NuGet, RPM, Debian, or generic HTTP file-repository compatibility.

CNCF Distribution satisfies the narrow registry requirement, but zot provides the same OCI foundation plus appliance-useful search, UI, integrity scrubbing, inline garbage collection, deduplication, artifact discovery, and optional trust/scanning features in one Go binary. zot is Apache-2.0, unprivileged, and a CNCF Sandbox project. These benefits better match the artifact-appliance direction accepted in the latest product discussion.

## Decision

Use a pinned zot release as the v1 OCI image and artifact data plane.

- Run one unprivileged zot pod with a dedicated ReadWriteOnce filesystem PVC on the appliance data volume. Do not introduce a registry database, Redis, LDAP, or registry-owned user store.
- Use traditional external bearer authentication. zot challenges clients with `GET /api/v1/registry/token`; the control plane validates an appliance API token, intersects requested repository scope with appliance RBAC, and issues a five-minute OCI access JWT.
- Podman, Skopeo, Buildah, Helm, and ORAS login use the appliance username as the Basic username and an appliance API token as the Basic password. The token endpoint verifies that the username/account matches the API-token owner. It never accepts an interactive password.
- The token endpoint accepts the standard `service`, repeated `scope`, `account`, `offline_token`, and supported client parameters. V1 returns only a short-lived access token and does not issue registry refresh tokens.
- Parse and normalize every requested scope and intersect it with current appliance RBAC. Never reflect an unvalidated scope string into the JWT. Initially sign only standard repository `pull` and `push` actions; deletion and lifecycle policy remain control-plane workflows.
- Do not configure zot `htpasswd`, LDAP, social login, OIDC login, anonymous access, or independent `accessControl` policy as a second appliance authorization plane. Traditional scoped bearer claims are the effective repository authorization input.
- Mount only the public verification material into zot. Keep the registry private signing key in the control plane. Use a registry-specific Ed25519 key only after the pinned-release spike proves static-cert verification, accepted claims, algorithm restrictions, and failure behavior.
- Use zot's static local verification-key mode. Planned rotation enters registry-auth maintenance, pauses new token issuance, waits five minutes plus allowed clock skew for old tokens to expire, installs the new zot public key, switches the control-plane signer, restarts and verifies zot, then resumes issuance. Emergency rotation skips the wait and invalidates outstanding registry tokens. Do not add a cloud secret manager merely to obtain multi-key rotation.
- Expose `/v2/*` directly to zot. The control plane and `zonctl` must not proxy image layers or generic artifact payloads.
- Support Buildah image publication, Podman image pull/runtime smoke tests, Skopeo inspect/copy/sync, and ORAS push/pull/attach/discover/copy. `zonctl artifact` may wrap ORAS-compatible behavior but must preserve OCI media types, digests, and referrers.
- Treat zot as authoritative for OCI manifests, tags, digests, referrers, and blobs. The control plane owns appliance-specific catalog metadata, policy, quotas, lifecycle intent, and audit, and reconciles rather than blindly trusting events.
- Start with filesystem storage using `commit: true`. Enable ext4 hard-link deduplication and garbage collection with a 24-hour delay only after filesystem, concurrency, backup, restore, and failure-injection tests prove their behavior. Pin explicit values rather than relying on upstream defaults.
- ADR 0010 selects the full zot image with enhanced search without CVE refresh, periodic scrub, internal metrics, and internal event delivery. Explicitly disable UI, management mutation APIs, user preferences, sync/mirroring, profiling, lint, image trust, and CVE scanning in the first release.
- UI remains deferred until browser authentication can reuse appliance identity without a second session authority, repository filtering is proven, and its indexes/state pass rebuild and recovery tests.
- Keep management, metrics, profiling, and debug endpoints off public ingress. Metrics may be exposed only inside the cluster to an appliance-owned collector.
- Manifest deletion and retention run only through control-plane-authorized workflows. Test referrer handling, GC delay, deduplication hard links, concurrent upload/delete, and backup interactions before enabling automatic reclamation.
- Pin image digest, source tag, configuration schema, transitive notices, checksums, SBOM, vulnerability report, and provenance in every appliance release.

## Product Boundary

Artifact Server v1 is:

```text
zot + OCI/ORAS + appliance control plane
```

It supports container images, Helm charts, SBOMs, signatures, provenance, binaries, archives, and other payloads represented as OCI artifacts with explicit media types.

Native package-manager protocols are not included. Future npm, Maven, PyPI, NuGet, RPM, or Debian support must be separate feature modules and adapters that reuse appliance identity, RBAC, audit, and lifecycle policy.

## Consequences

The appliance gains artifact search and management building blocks without adding Harbor's service stack or Nexus/JFrog licensing constraints. It also accepts more zot-specific behavior than the previous deliberately narrow Distribution choice. Extension configuration, metadata rebuild, security advisories, and upgrade compatibility therefore become part of the supported appliance contract.

CNCF Sandbox status is not equivalent to a vendor support SLA. The release process must define our own supported-version window, CVE response target, upgrade tests, and fallback/export path. Because storage is OCI layout and the wire API is OCI Distribution, migration to another conformant registry remains feasible but must still be tested.

## Rating And Acceptance Gate

Rate the pinned zot candidate using evidence, not project-level marketing. Score each category from 0 to 5 and apply the weight below.

| Category | Weight | Required evidence |
| --- | ---: | --- |
| Architecture and control-plane fit | 15% | No duplicate identity/RBAC; external scoped bearer flow; clean adapter boundary |
| Security and isolation | 20% | Unprivileged runtime, restricted ingress, algorithm/claim validation, key rotation, negative auth tests |
| Appliance footprint | 10% | Measured CPU, memory, startup, disk/inode use, and no mandatory external services |
| OCI and ORAS capability | 15% | Buildah/Podman/Skopeo and ORAS/referrers/media-type compatibility; conformance evidence |
| Product capability and UX | 10% | Browser auth, UI/search RBAC filtering, accessibility, catalog usability, and management-route isolation |
| Operations and recovery | 15% | Backup/restore, scrub, GC, dedupe, disk-full, corruption, restart, upgrade and downgrade/export tests |
| Maturity and supportability | 5% | Release cadence, security policy/advisories, maintainer activity, supported-version policy |
| Licensing and supply chain | 5% | Apache-2.0 baseline, transitive scan, SBOM, signed/pinned provenance, redistributable notices |
| Extensibility without lock-in | 5% | OCI-layout export, standard APIs, adapter isolation, migration proof |

Acceptance requires at least 4.0/5 weighted overall, no score below 3 in security or operations, and every mandatory conformance test passing. A numerical score never overrides a failed security or recovery gate.

## Conformance Gate

- Pin the full zot image by digest and prove the exact ADR 0010 enabled/disabled extension set.
- Prove Podman, Skopeo, and Buildah login, pull, push, inspect, copy, logout, denied scope, malformed scope, cross-repository mount, token expiry, user disable, API-token revocation, and anonymous denial.
- Prove ORAS push, pull, attach, discover, copy, custom media types, referrers, large artifacts, interrupted upload, and digest preservation.
- Verify signature algorithm allowlisting, issuer, audience/service, subject, `kid`, time claims, `access` type/name/actions, path normalization, duplicate claims, and rejection of malformed or confused-deputy tokens.
- Prove the bounded planned signing-key maintenance rotation and emergency invalidation using only appliance-local components, including failure recovery at every step.
- Verify enhanced search cannot reveal unauthorized repositories and that UI/management routes are disabled. Verify every enabled extension's route, auth, persistence, egress, and recovery behavior. Browser login/logout, accessibility, and large-catalog UI tests become mandatory before any future UI enablement.
- Test restart, concurrent upload/delete, disk and inode exhaustion, read-only volume, bit corruption and scrub, dedupe hard links, GC delay, retention/referrers, quota behavior, storage backup, and clean-node restore.
- Test N-1 upgrade, failed upgrade recovery, configuration-schema validation, metadata/index rebuild, and OCI export or migration to a second registry.
- Run the applicable OCI Distribution conformance suite and black-box tests against the exact ingress/TLS topology shipped by the appliance.
- Measure idle and representative-load CPU, memory, latency, throughput, disk amplification, and inode growth on the minimum supported appliance size.
- Prove operation with public egress denied. Scanner databases and every other update input arrive only through the signed appliance bundle; background network refresh remains disabled.

## Alternatives

- CNCF Distribution v3: viable fallback if zot fails its gate; narrower and more mature, but requires us to build or add more artifact discovery and operational capability.
- Harbor: rejected for v1 because it duplicates identity, RBAC, token, UI, PostgreSQL, and Redis responsibilities.
- Pulp: deferred for native multi-format package protocols; materially larger and GPL/transitive-license review is required.
- Nexus and JFrog: rejected because their product/licensing and identity/token models do not fit the base appliance.

## References

- [zot project overview](https://zotregistry.dev/)
- [zot authentication and authorization](https://zotregistry.dev/v2.1.15/articles/authn-authz/)
- [zot configuration](https://zotregistry.dev/v2.1.18/admin-guide/admin-configuration/)
- [zot storage planning](https://zotregistry.dev/articles/storage/)
- [zot OCI/ORAS data-path guide](https://zotregistry.dev/v2.1.15/user-guides/user-guide-datapath/)
- [OCI Distribution Specification](https://github.com/opencontainers/distribution-spec)
- [ORAS project](https://oras.land/)

# Appliance Decision Register

This register tracks architecture decisions for the appliance control plane. An accepted decision remains changeable through a superseding ADR.

| ADR | Decision | Status | Accepted | Implementation gate |
| --- | --- | --- | --- | --- |
| [0001](adr/0001-dedicated-k3s-appliance.md) | Dedicated, product-managed, single-node K3s appliance | Accepted | 2026-07-03 | Host preflight and clean install/uninstall test |
| [0002](adr/0002-nexus-identity-and-storage.md) | Nexus Community Edition with Postgres and token shadow principals | Superseded by ADR 0007 | 2026-07-03 | Rejected after external-token and licensing review |
| [0003](adr/0003-trusted-build-boundary.md) | Trusted-only Containerfile builds using ephemeral rootless Buildah | Accepted with validation gate | 2026-07-03 | K3s Buildah isolation, storage, output, and cleanup spike |
| [0004](adr/0004-control-plane-sqlite.md) | Single-replica control plane using SQLite behind repository interfaces | Accepted | 2026-07-03 | Volume, disk-full, online-backup, and clean restore tests |
| [0005](adr/0005-secrets-keys-and-tls.md) | Installer-owned, purpose-separated keys and explicit TLS modes | Accepted | 2026-07-03 | Rotation and restore tests |
| [0006](adr/0006-backup-upgrade-and-recovery.md) | Off-appliance backups and restore-based rollback | Accepted | 2026-07-03 | Clean-node restore and N-1 upgrade tests |
| [0007](adr/0007-cncf-distribution-registry.md) | CNCF Distribution v3 with control-plane-issued OCI tokens | Superseded by ADR 0008 | 2026-07-03 | Replaced after the artifact-appliance review |
| [0008](adr/0008-zot-oci-artifact-registry.md) | zot for OCI images and generic OCI/ORAS artifacts | Accepted with conformance gate | 2026-07-03 | Auth, ORAS, extensions, storage, restore, upgrade, and air-gap tests |
| [0009](adr/0009-oci-toolchain.md) | Buildah, Podman, Skopeo, ORAS, zot, and Helm with explicit responsibilities | Accepted with compatibility gates | 2026-07-03 | Shared auth, trust policy, client matrix, build corpus, and air-gap tests |
| [0010](adr/0010-v1-security-and-operations-defaults.md) | Concrete v1 auth, MCP, RBAC, HTTP, audit, telemetry, supply-chain, support, and zot defaults | Accepted with validation gates | 2026-07-03 | Security-policy, failure, air-gap, and minimum-host validation suites |
| [0011](adr/0011-argo-workflows-engine.md) | Namespace-scoped Argo Workflows behind the control plane in the complete appliance | Accepted with validation gate | 2026-07-03 | CRD/RBAC, template admission, lifecycle, restart, upgrade, and air-gap tests |
| [0012](adr/0012-offline-first-appliance.md) | One complete air-gap package with no install-time or runtime public-network dependency | Accepted | 2026-07-03 | Egress-denied install, operation, update, restore, and conformance suites |

## Release Data Still To Pin

Exact dependency versions and image digests are release data, not permanent architecture decisions. Phase 0 must pin supported versions of Go, K3s/Kubernetes, Traefik, Argo Workflows, zot, Buildah, Podman, Skopeo, ORAS, Helm, Syft, Grype, release-only Cosign, the SQLite driver, and the MCP protocol in a machine-readable compatibility manifest.

Security and operations defaults are accepted in ADR 0010. Phase 0 still pins exact component versions, image and binary digests, transitive notices, scanner database revision, minimum-host sizing, and compatibility evidence. These are release facts rather than open architecture decisions.

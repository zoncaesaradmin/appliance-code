# Phase 0 Compatibility Candidates

- Snapshot date: 2026-07-03
- Status: Candidate inputs for E0-01, not released pins

Use this set to start compatibility testing. The release compatibility manifest replaces each version with verified source/release identity, checksums, image or binary digests, licenses/notices, SBOM, provenance, and test evidence. Never consume `latest` or a mutable action/tag.

| Component | Candidate | Rationale and gate |
| --- | --- | --- |
| Go | 1.26.4 | Current supported Go 1.26 security patch; run local macOS and Linux race/build/test lanes |
| Host OS | Ubuntu Server 24.04 LTS amd64 | Accepted appliance baseline; validate kernel, cgroup v2, user namespaces, ext4, AppArmor, and power-loss behavior |
| K3s | v1.36.1+k3s1 | Current stable release at snapshot; includes Kubernetes 1.36.1, Traefik 3.6.13, containerd 2.2.3-k3s1, etcd 3.6.7-k3s1, and SQLite 3.53.0 |
| Traefik | K3s-bundled 3.6.13 | Avoid a second ingress lifecycle; verify route, TLS, headers, limits, and NetworkPolicy behavior |
| Argo Workflows | Pin during E0-01 | Select a supported stable release compatible with the pinned Kubernetes API; verify CRDs, namespace-scoped controller, executor, Workflow TTL, RBAC, upgrade, air-gap, and controller-restart behavior |
| zot | v2.1.18 full image | Current documented stable line; pin image digest and prove the ADR 0010 extension profile |
| Podman | v5.8.2 | Conservative stable 5.8 patch for runtime/client tests rather than adopting a new major before evidence |
| Buildah | v1.43.1 | Version bundled by Podman 5.8.2; rootless K3s isolation/storage gate remains mandatory |
| Skopeo | v1.23.0 | Current stable image utility; verify auth-file, policy, signing, copy, and sync compatibility with the selected containers configuration |
| ORAS CLI | v1.3.2 | Current stable OCI artifact client; verify distribution-spec 1.1/referrers behavior with zot |
| Helm | v4.2.0 | Current stable major; chart must also render/install through the pinned K3s Helm controller |
| Syft | v1.44.0 | Current immutable-release SBOM candidate; produce CycloneDX JSON and SPDX JSON |
| Grype | v0.112.0 | Current immutable-release vulnerability scanner candidate; pin and sign the exact offline DB bundle separately |
| Cosign | v3.0.6, release engineering only | Current security-fix release candidate for manifest/provenance/blob signatures; never installed as an appliance runtime service |
| SQLite Go driver | `modernc.org/sqlite` v1.50.1 | CGo-free candidate for macOS/Linux local builds; validate supported SQLite features, backup API path, WAL, locking, performance, and transitive footprint before accepting |
| MCP | 2025-11-25 | Pinned Streamable HTTP protocol baseline; v1 uses the ADR 0010 API-token compatibility authorization mode |

## Selection Rules

- Prefer a stable security-patched release over a release candidate or newly published major.
- Keep the containers tool family coherent. If a candidate forces incompatible `containers/image`, storage, auth-file, or policy behavior, move the complete tested set rather than one binary in isolation.
- Do not automatically advance K3s or its bundled components during packaging. A new patch starts a compatibility run and produces a new manifest.
- Verify upstream signatures/attestations and checksums before mirroring. Mirror by digest into the release staging registry and retain upstream provenance.
- Reject any candidate with an unresolved Critical finding. Apply ADR 0010's signed, time-limited exception process for High findings.
- Record rejected candidates and reasons so future upgrades do not repeat failed spikes.

## Required E0-01 Output

The machine-readable manifest must include:

- component name, version, source URL, source revision, license, and support/EOL information
- binary/archive checksum or OCI digest and supported architecture
- upstream signature/attestation identity and local verification result
- transitive SBOM and vulnerability/license policy result
- configuration schema/version and compatibility dependencies
- controlled release-assembly source and final air-gap bundle location
- test-suite evidence ID and acceptance/rejection status

## References

- [Go release history](https://go.dev/doc/devel/release)
- [K3s releases](https://github.com/k3s-io/k3s/releases)
- [Argo Workflows releases](https://github.com/argoproj/argo-workflows/releases)
- [zot documentation](https://zotregistry.dev/)
- [Podman releases](https://github.com/containers/podman/releases)
- [Buildah releases](https://buildah.io/releases/)
- [Skopeo](https://github.com/containers/skopeo)
- [ORAS CLI](https://github.com/oras-project/oras)
- [Helm releases](https://github.com/helm/helm/releases)
- [Syft](https://github.com/anchore/syft)
- [Grype](https://github.com/anchore/grype)
- [Cosign releases](https://github.com/sigstore/cosign/releases)
- [modernc SQLite](https://gitlab.com/cznic/sqlite)

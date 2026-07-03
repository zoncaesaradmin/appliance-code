# Registry Options And zot Rating

This review incorporates the latest `Lightweight Artifactory Setup` decision: use zot as the base OCI image and generic OCI/ORAS artifact registry, while keeping native package-manager protocols outside v1.

The appliance requirements are:

- container-native, unprivileged K3s deployment
- appliance-owned users, API tokens, RBAC, revocation, and audit
- standard OCI bearer challenge with control-plane-issued repository scopes
- Buildah, Podman, Skopeo, Helm, and generic ORAS artifact workflows
- low operational footprint and an air-gap-capable release
- redistribution-friendly licensing with complete release notices
- no mandatory commercial/community-edition feature split

License notes are engineering inputs, not legal advice. Every release still needs exact-version SBOM, provenance, transitive license and vulnerability scans, required notices, and release approval.

## Product Comparison

| Option | License baseline | Runtime and identity footprint | Artifact capability | Decision |
| --- | --- | --- | --- | --- |
| zot | Apache-2.0 | One unprivileged Go binary plus storage; external bearer verification | OCI images, Helm and generic OCI/ORAS artifacts; optional UI/search/scrub/GC/trust/scanning | Selected for v1, gated |
| CNCF Distribution v3 | Apache-2.0; docs CC-BY-4.0 | One Go registry plus storage; native external token service | OCI images and artifacts; deliberately narrow feature set | First fallback |
| Harbor | Apache-2.0 plus component notices | Core, portal, job service, registry, PostgreSQL, Redis and optional scanners; owns projects/RBAC/token service | OCI images and artifacts with broad management features | Rejected as duplicate control plane and heavier appliance |
| Pulp with `pulp_container` | GPL-2.0-or-later for pulpcore; plugin/transitive review required | Python/Django workers, PostgreSQL, content storage and plugins | Multi-format through plugins; external container token server supported | Deferred native-package candidate |
| Project Quay | Apache-2.0 plus component licenses | Quay services, database, storage and optional Clair/Redis; owns organizations and auth | OCI images and artifacts | Rejected as heavier duplicate control plane |
| Nexus Repository | Vendor Community/Pro split plus bundled notices | Product server, supported database and blob storage; owns its registry token realm | Multi-format | Superseded/rejected for token architecture and product/licensing fit |
| JFrog Artifactory | Commercial/vendor licensing | Multi-service vendor platform and vendor-owned identity/token model | Multi-format | Rejected for base-appliance licensing and footprint |
| zot/Distribution with managed `htpasswd` | Product license unchanged | Adds credential-file reconciliation and another secret lifecycle | OCI only; authentication cannot cleanly represent appliance repository-action RBAC | Rejected authentication mode |

## Selected Architecture

```text
Buildah / Podman / Skopeo / ORAS / Helm / zonctl
                  |
                  | 1. OCI request
                  v
                 zot
                  |
                  | 2. WWW-Authenticate realm=/api/v1/registry/token
                  v
         Appliance control plane
                  |
                  | 3. validate appliance API token
                  | 4. intersect requested scope with appliance RBAC
                  | 5. issue five-minute registry JWT
                  v
       zot verifies scope and serves OCI content
```

The control plane does not proxy payloads. zot receives no appliance password database and cannot mint appliance credentials. Traditional bearer-token `access` claims carry the already-authorized repository actions.

## Why zot

zot retains the small, single-binary shape of Distribution while adding functions useful to a self-contained artifact appliance: OCI-layout storage, ORAS workflows, enhanced search, UI, scrubbing, inline garbage collection, deduplication, trust, metrics, and optional vulnerability data. It is Apache-2.0, unprivileged, multi-architecture, and a CNCF Sandbox project.

Those additions are not free. ADR 0010 selects the full image with enhanced search, scrub, internal metrics, and internal events while disabling UI, management mutation, CVE refresh/scanning, sync, profiling, lint, user preferences, and image trust. CNCF Sandbox status also means we own the product support policy and cannot imply a commercial SLA.

## Rating Model

Use a 0-to-5 score backed by test evidence. The provisional score below measures architectural suitability, not production acceptance.

| Category | Weight | Provisional rating | Rationale and missing proof |
| --- | ---: | ---: | --- |
| Architecture/control-plane fit | 15% | 4.5 | External scoped bearer model and no mandatory identity DB fit well; exact claim and extension boundaries need black-box proof |
| Security/isolation | 20% | 3.5 | Unprivileged operation and explicit extension disablement are strong; planned key rotation, route isolation and negative JWT behavior still require test evidence |
| Appliance footprint | 10% | 4.5 | One full-image Go binary and PVC with no mandatory external service; benchmark the selected configuration |
| OCI/ORAS capability | 15% | 4.5 | OCI-native storage and documented ORAS/referrers flows; run conformance and client matrix |
| Product capability and UX | 10% | 3.5 | Enhanced search and artifact discovery fit v1; UI is deliberately deferred until appliance-auth integration is proven |
| Operations/recovery | 15% | 3.5 | Inline GC, dedupe and scrub are attractive; restore consistency, hard-link backup, corruption and upgrade behavior need proof |
| Maturity/supportability | 5% | 3.0 | Active CNCF Sandbox project with no assumed vendor SLA; our N/N-1 and 12-month support policy still needs operational evidence |
| Licensing/supply chain | 5% | 4.0 | Apache-2.0 baseline is favorable; exact full-image transitive licenses, SBOM and provenance still require review |
| Extensibility/no lock-in | 5% | 4.5 | OCI wire and layout standards plus adapter isolation offer a credible export path; migration drill remains open |
| **Weighted provisional result** | **100%** | **3.95/5** | **Promising, but not yet over the acceptance threshold without spike evidence** |

Additional rating evidence that must be captured:

- Exact full-image digest and proof that only the ADR 0010 extension inventory is reachable
- CPU, memory, startup, throughput, disk amplification, and inode growth
- Release cadence, maintained-version window, security advisory handling, and time-to-patch
- JWT algorithm allowlist, `kid`, issuer/audience, claim parsing, key rotation, and revocation latency
- Search authorization filtering and proof that UI/management routes are disabled
- Browser login/logout, accessibility, large-catalog usability, and appliance-session integration before any future UI enablement
- GC, dedupe, scrub, retention, referrers, concurrent mutation, and quota behavior
- Backup/restore consistency, metadata/index rebuild, upgrade/downgrade, and migration/export
- Air-gap handling for CVE databases, mirrors, trust roots, and all background egress
- Multi-architecture image provenance and transitive license/vulnerability findings

Acceptance requires at least 4.0/5 weighted overall, no security or operations score below 3, and every mandatory gate in [ADR 0008](adr/0008-zot-oci-artifact-registry.md) passing. Scores communicate tradeoffs; they never waive a failed security or recovery test.

## Artifact Boundary

V1 supports container images, Helm charts, SBOMs, signatures, provenance, binaries, archives, and other explicitly typed OCI artifacts. `zonctl artifact push/pull` may provide a friendlier wrapper over OCI/ORAS.

V1 does not provide native `npm install`, Maven dependency resolution, PyPI, NuGet, apt, yum/dnf, or generic package-repository semantics. If those become requirements, add separate protocol adapters or feature modules that reuse appliance identity and authorization. Pulp is the first open-source multi-format candidate to reassess, including GPL and plugin/transitive-license implications.

## References

- [zot project overview and image variants](https://zotregistry.dev/)
- [zot authentication and authorization](https://zotregistry.dev/v2.1.15/articles/authn-authz/)
- [zot configuration and extensions](https://zotregistry.dev/v2.1.18/admin-guide/admin-configuration/)
- [zot storage planning](https://zotregistry.dev/articles/storage/)
- [zot ORAS artifact workflows](https://zotregistry.dev/v2.1.15/user-guides/user-guide-datapath/)
- [CNCF Distribution](https://distribution.github.io/distribution/)
- [Harbor installation and components](https://goharbor.io/docs/main/install-config/)
- [Pulp external token authentication](https://pulpproject.org/pulp_container/docs/admin/learn/authentication/)
- [Project Quay](https://www.projectquay.io/index.html)

# Product And Release Repository Boundary

## Decision

The appliance uses two repositories:

- `appliance-code` is private and owns product source, behavior, deployment contracts, and immutable release inputs.
- `appliance-release` is public and owns installation, host lifecycle, bundle assembly, public documentation, and distribution evidence.

The boundary is an artifact handoff, not a source-code dependency. `appliance-release` must never need read access to this repository to install or validate a released appliance.

## Ownership Matrix

| Concern | `appliance-code` | `appliance-release` |
| --- | --- | --- |
| Go control-plane source and tests | Owns | Never contains |
| REST, MCP, authn/authz, and audit behavior | Owns | Documents released behavior |
| SQLite schema and migrations | Owns and embeds/packages | Invokes through supported application lifecycle only |
| zot and registry integration behavior | Owns | Packages pinned validated image/config inputs |
| Argo workflow templates and adapter | Owns | Packages approved product inputs when enabled |
| Canonical Helm chart and schema | Owns | Installs the exact packaged chart; never forks it |
| Development manifests and local Go lane | Owns | Out of scope |
| Component compatibility evidence | Produces | Verifies and publishes accepted matrix |
| Control-plane OCI image | Builds, signs, and attests | Verifies, mirrors/packages, and installs |
| K3s host installation/configuration | Defines required contract | Implements and tests |
| Host preflight/remediation | Defines minimum requirements | Implements user-facing checks and safe remediation |
| Air-gap bundle assembly | Supplies product inputs | Owns |
| Upgrade, repair, uninstall, and restore UX | Supplies application hooks/constraints | Owns end-to-end orchestration |
| Signing policy | Defines required identities/claims | Verifies; release pipeline signs final manifest |
| Release notes, support matrix, and notices | Supplies product facts | Owns public presentation and distribution |
| Secrets and production credentials | Never publishes | Never stores; installer generates or accepts them at install time |

## Release Input Contract

Each candidate from `appliance-code` publishes one immutable input set identified by product version and source revision:

```text
  release-input/
  release-input.json
  control-plane.oci.tar.zst
  appliance-chart-<version>.tgz
  argo-workflows-chart-<version>.tgz
  argo-crds/
  argo-controller.oci.tar.zst
  argo-executor.oci.tar.zst
  configuration.schema.json
  compatibility.json
  checksums.txt
  sbom/
  provenance/
  notices/
  tests/
```

`release-input.json` includes:

- product version, source revision, build identity, creation time, and schema version
- digest and size of every file
- control-plane image digest and supported architectures
- Argo controller/executor image digests and supported architectures when the
  Argo workflow engine is enabled in the release-input set
- chart version, application version, and required values-schema version
- Argo version, CRD bundle identity, and workflow-controller chart identity
- database migration compatibility and minimum/maximum source version
- required K3s/Kubernetes, Traefik, zot, Buildah, Podman, Skopeo, ORAS, Syft, Grype, and Helm compatibility identities
- configuration and bootstrap contract versions
- release-signing and verification identities
- conformance-test entrypoints and expected evidence schema

The set is signed and immutable. Rebuilding any member creates a new candidate identity even if the semantic product version has not changed.

## Release Output Contract

`appliance-release` turns one accepted input set into one complete air-gap bundle containing supported-host package prerequisites, K3s, K3s platform images, product/dependency OCI images, chart, installer, schemas, scanner data, notices, SBOMs, provenance, and checksums.

There is no connected production package in v1. A machine with internet access may install the same air-gap bundle, but installation and runtime never fetch missing components or switch behavior based on connectivity.

The release output also includes:

- an upgrade bundle from every supported source version
- public release metadata, support matrix, release notes, and verification instructions

V1's supported production path is a dedicated Linux host with a product-managed K3s installation. A workload-only install onto an arbitrary existing cluster is not a supported v1 path. Keeping the chart separately installable helps development and future qualification but does not bypass host or compatibility checks.

## Change Rules

- A product behavior, API, schema, chart, Workflow template, or security-policy change starts in `appliance-code`.
- A host check, installer flow, bundle layout, public documentation, or release-channel change starts in `appliance-release`.
- Cross-boundary changes update the `release-input.json` schema first and require producer and consumer compatibility tests.
- The release repo may reject an input candidate but may not patch it. Fixes return to `appliance-code` and produce a new candidate.
- No release selects mutable tags or downloads unverified content at install time.
- Public topology and dependency versions are not secrets. Keys, credentials, customer configuration, private source, and internal CI coordinates are never release-repo content.

## Joint Release Gate

A version is releasable only when:

1. `appliance-code` conformance and security gates pass for the immutable input set.
2. `appliance-release` verifies every signature, digest, license notice, SBOM, provenance statement, and compatibility tuple.
3. Fresh installs from the air-gap bundle pass on every supported host baseline with public egress denied.
4. Upgrade, failed-upgrade recovery, backup, clean-node restore, safe uninstall, and support-bundle tests pass.
5. The installed API, MCP, and OCI behavior passes the product-supplied black-box suite.

Neither repository can declare a production release independently.

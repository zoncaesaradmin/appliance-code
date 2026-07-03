# Appliance Development Invariants

These rules apply to all implementation and documentation in this repository.

## Offline-First Product

- V1 has one production package: the complete air-gapped appliance bundle produced by `appliance-release`.
- Installation, startup, normal operation, authentication, builds, registry use, backup, restore, diagnostics, and upgrade must not require public internet access.
- Do not add a connected installer, install-time downloader, remote package repository requirement, phone-home behavior, external license check, dynamic plugin fetch, or background internet updater.
- Every executable, required host package, OCI image, K3s artifact, chart, CRD, scanner database, migration, schema, and static asset required by the released product must be pinned, verified, and present in the signed air-gap bundle.
- Runtime components must remain functional with public egress denied. Tests must exercise this condition.
- Product updates arrive only as new signed offline bundles. Components must not self-update.

## Allowed Network Use

- Clients may access the appliance over its configured local or enterprise network.
- User-directed workflows may access explicitly allowlisted operator-provided services such as an internal Git server, backup target, DNS/NTP service, or future enterprise identity provider. These are deployment inputs, not hidden product dependencies.
- Build definitions must not assume public package repositories or public base-image registries. Required build inputs must be available from zot, the submitted context, or explicitly configured internal allowlists.
- Controlled release assembly and development dependency acquisition may use the internet outside the installed appliance, but those actions must produce pinned, verified bundle inputs and must never occur during installation or runtime.

## Packaging Boundary

- This repo owns product code, the canonical Helm chart, workflow templates, compatibility evidence, and signed immutable release inputs.
- `appliance-release` owns the one complete air-gap bundle and host lifecycle tooling.
- Preserve modular interfaces for future change, but do not add package profiles or alternate connected/offline implementation paths in v1.

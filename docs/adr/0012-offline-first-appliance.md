# ADR 0012: Offline-First Appliance And Single Air-Gap Package

- Status: Accepted
- Date: 2026-07-03

## Context

The appliance is intended primarily for disconnected environments. Maintaining connected and air-gap installers would create two acquisition paths, two failure surfaces, and avoidable release drift. Runtime downloads, background refresh, external license checks, and silent telemetry would also violate the product's deployment purpose.

## Decision

- V1 has one production distribution: a complete signed air-gap bundle for the supported host platform.
- The bundle contains the pinned package closure needed on the supported host baseline, K3s binaries and platform images, the control plane, zot, Argo controller/executor, Buildah and utility task images, Helm chart, CRDs, scanner database, configuration schemas, migrations, verification keys, SBOMs, provenance, notices, and offline conformance tests.
- Installation, startup, normal operation, authentication, registry use, builds, backup, restore, diagnostics, and upgrade require no public internet access and must pass with public egress denied.
- There is no connected installer. The same bundle may be installed on a network-connected host, but connectivity does not change acquisition or runtime behavior.
- Components do not self-update, refresh from public services, phone home, perform external license checks, or dynamically fetch plugins. Updates arrive only as new signed offline bundles.
- Controlled release assembly may acquire upstream artifacts in a connected trusted environment. It verifies, pins, records, and closes every dependency before publishing the bundle.
- User-directed access to explicitly allowlisted internal Git servers, DNS/NTP, backup targets, clients, or future enterprise identity systems is permitted. These are operator-provided deployment inputs, not hidden product dependencies.
- Build recipes cannot assume public package repositories or base-image registries. Required inputs must come from the submitted context, appliance zot, or explicitly configured internal allowlists.
- V1 ships one complete topology including Argo; package profiles and workload-only installation are deferred.

## Consequences

The release is larger, assembly and vulnerability-data updates require publishing a new bundle, and qualification must test the entire dependency closure. In return, one artifact has one reproducible install path and behaves identically regardless of internet connectivity.

## Verification

- Install on a clean supported host while public egress is technically blocked.
- Reboot, build, scan, push/pull artifacts, back up, restore, diagnose, and upgrade without public network access.
- Monitor DNS and network attempts and fail the release on unexpected public destinations.
- Remove every preloaded dependency in turn and prove installation fails before partial activation with an exact integrity error rather than attempting a download.
- Verify all background update, telemetry, plugin, and license-check paths are absent or disabled.
- Verify a new signed bundle is the only supported update mechanism.

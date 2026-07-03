# ADR 0006: Backup, Upgrade, And Recovery

- Status: Accepted
- Date: 2026-07-03

## Context

The appliance contains K3s state, control-plane state and keys, and OCI registry content. A same-disk copy is not a disaster-recovery backup.

## Decision

Initial service objectives are an RPO of 24 hours and RTO of 4 hours for a supported single-node replacement. Both are configurable toward stricter targets after capacity testing.

- Configure K3s as a single-node embedded-etcd cluster so the release can use scheduled K3s etcd snapshots instead of copying a live K3s SQLite datastore.
- A complete recovery set contains: K3s snapshot and server token, control-plane SQLite online backup, all required key material, Argo CRDs/controller configuration and workflow-template release identity, the zot storage root and required extension state, certificates, sanitized configuration/Helm values, compatibility manifest, and checksums.
- In-flight Argo tasks are quiesced where possible. Clean-node restore does not promise process-level continuation: the control plane reconciles each operation to a safely resumable or explicit interrupted state, and digest-bound publication prevents duplicate or ambiguous output.
- Production setup requires an off-appliance backup destination. Local staging on the appliance is temporary and does not satisfy backup health.
- Run a daily coordinated backup and an additional backup immediately before upgrade or destructive administration. Default retention is 7 daily and 4 weekly recovery sets.
- For v1, favor correctness over zero downtime: enter maintenance mode, stop build admission, quiesce zot background GC/scrub/index work and scale it down, snapshot/copy the registry storage root, back up control-plane state and keys, snapshot K3s, validate checksums and SQLite integrity, then resume services.
- Encrypt recovery sets before transfer and authenticate their manifest. Never place the only backup-encryption recovery key inside the same encrypted set.
- Restore targets a clean, supported host and verifies component versions before writing data. Restore runs application smoke checks, registry catalog/manifest/blob integrity checks, login, RBAC, Podman pull, and a disposable build when builds are enabled.
- Support upgrades only from the immediately previous supported appliance release (`N-1` to `N`) in v1. Pin every image by digest and carry a machine-readable compatibility manifest.
- Upgrade order is installer/K3s compatibility preflight, complete backup, infrastructure changes, stateful dependencies, control-plane migration, reconciliation, and smoke tests.
- Control-plane migrations are forward-only. Rollback after state or storage-format migration means restoring the complete pre-upgrade recovery set with the previous release, not merely rolling container tags backward.
- Break-glass recovery is node-local, requires root/operator access, writes a high-severity audit event when the database is available, and never bypasses backup compatibility checks.

## Consequences

Daily coordinated backups may cause a maintenance window. Zero-downtime backup and broader upgrade spans are later improvements. The release cannot claim successful backup merely because files were produced; clean-node restore is the acceptance test.

## Verification

- Scheduled and pre-upgrade backup tests
- Corrupt, incomplete, wrong-key, and wrong-version recovery-set rejection
- Clean-node restore within RTO and measured recovery-point reporting
- Interrupted backup and interrupted upgrade tests
- Automated N-1 to N upgrade and pre-upgrade restore rollback

## References

- [K3s backup and restore](https://docs.k3s.io/datastore/backup-restore)
- [zot storage planning](https://zotregistry.dev/articles/storage/)
- [zot configuration and background tasks](https://zotregistry.dev/v2.1.18/admin-guide/admin-configuration/)

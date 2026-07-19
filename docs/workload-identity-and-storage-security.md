# Workload Identity And Storage Security

The appliance runs K3s rootful in the initial supported baseline, but appliance
application containers and workflow pods run as non-root workloads.

## Numeric Identity Registry

Numeric IDs are part of the product compatibility contract and must remain
stable across releases.

| Component | UID | Primary GID | Notes |
| --- | ---: | ---: | --- |
| Control plane | 10001 | 10001 | Main API service |
| Control plane UI | 10002 | 10002 | Browser-facing UI service |
| Argo workflow controller wrapper | 65532 | 65532 | Upstream non-root controller identity |
| Builder/workspace workflow pods | 10010 | 10010 | Appliance-generated Argo workflow workloads |
| Shared appliance filesystem group | n/a | 20000 | Supplemental group for shared writable storage |

Do not reuse a service UID as the shared filesystem GID. Shared writable mounts
must use GID `20000`, `fsGroup: 20000`, and
`fsGroupChangePolicy: OnRootMismatch` unless a future ADR deliberately changes
the registry.

## Storage Rules

- Give each service its own PVC unless the storage is genuinely shared.
- Keep writable host paths rare and documented. `/var/log/appliance` and the
  host-visible workspace root `/data/zon/workspaces` are intentional product
  interfaces, not generic scratch space.
- Use setgid directories and group-writable modes such as `2770` for shared
  writable paths.
- Never use `chmod 777` as the normal ownership solution.
- Keep application container root filesystems read-only and mount only explicit
  writable paths.
- Use root init containers only as narrow ownership-preparation or migration
  mechanisms.

## Validation Expectations

Chart, installer, and diagnostic changes that touch UID/GID, PVCs, host paths,
or ownership behavior must validate fresh install, upgrade, rollback, backup
restore, and machine migration paths. Health diagnostics and support bundles
must make storage ownership and writeability failures visible enough to debug
without manual cluster surgery.

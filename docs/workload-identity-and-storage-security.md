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
| Artifact server | 10003 | 10003 | Offline registry (zot wrapper) |
| LAN DNS (CoreDNS) | 10004 | 10004 | appliance-dns chart |
| Host agent | 10005 | 10005 | In-cluster host-agent pod |
| Inference runtime | 10006 | 10006 | appliance-inference chart |
| Automation runtime | 10007 | 10007 | Metadata bundle + DSL execution |
| Workflow controller wrapper | 65532 | 65532 | Upstream non-root controller identity |
| Builder/workspace workflow pods | 10010 | 10010 | Appliance-generated workflow workloads |
| Shared appliance filesystem group | n/a | 20000 | Supplemental group for shared writable storage |

Do not reuse a service UID as the shared filesystem GID. Shared writable mounts
must use GID `20000`, `fsGroup: 20000`, and
`fsGroupChangePolicy: OnRootMismatch` unless a future ADR deliberately changes
the registry. Never reuse a service UID across components.

## Storage Rules

- Give each service its own PVC unless the storage is genuinely shared.
- Keep writable host paths rare and documented. `/data/zon/logs` and the
  host-visible workspace root `/data/zon/workspaces` are intentional product
  interfaces, not generic scratch space.
- Use setgid directories and group-writable modes such as `2770` for shared
  writable paths.
- Runtime service log directories under `/data/zon/logs/<service>` are an
  operator-facing inspection interface, not general shared write storage. Keep
  them service-owner writable, but host-user readable/traversable (`2755`) so
  appliance operators can inspect logs without joining numeric Kubernetes
  groups or using root for normal debugging.
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

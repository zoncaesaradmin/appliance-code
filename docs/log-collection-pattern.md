# Log Collection Pattern

## Purpose

This document defines the appliance runtime log-collection pattern for the
target device.

The goal is:

- a predictable host path for operators to inspect logs
- one appliance-owned base directory for runtime logs
- a simple service-oriented organization under that base directory
- continued use of container `stdout`/`stderr`
- a phased path toward application-specific structured log files written by
  the Go services through the shared `platformkit/logging` module

This is a runtime log collection and operator-debugging pattern. It is not the
same thing as the control-plane SQLite state directory, `zonctl` state, or the
support-bundle output directory.

## Current Implementation Status

As of July 16, 2026, the first implementation slice covers the two
always-running Go services we build and ship directly:

- control plane
- UI service

They now have:

- a fixed host log root at `/var/log/appliance`
- service-specific subdirectories under that root
- startup scripts that mirror process `stdout` and `stderr` into:
  - `stdout.log`
  - `stderr.log`
  - `startup.log`

The Argo Workflow Controller and other third-party images are not yet moved to
this same startup-script pattern. They still rely on normal Kubernetes runtime
logs for now and remain a follow-on step.

## Fixed Host Path

The appliance runtime log root should be:

```text
/var/log/appliance
```

This path should be fixed for v1.

It should not be:

- user-configurable
- exposed as a product setting
- made a Helm values input
- varied by appliance profile

The application and deployment wiring should treat this as a product
constant.

## Current Runtime Reality

Today, the important always-running Go services log to `stdout`:

- control plane
- UI service
- Argo Workflow Controller

That means the practical runtime sources today are:

- `kubectl logs`
- K3s/container runtime log files under the normal host container log paths
- `journalctl -u k3s` for K3s/platform-level failures

Useful target-host commands today:

```bash
sudo kubectl get pods -A
sudo kubectl -n appliance-system logs deploy/control-plane
sudo kubectl -n appliance-system logs deploy/control-plane-ui
sudo kubectl -n workflows logs deploy/appliance-argo-workflows
sudo kubectl -n appliance-builds get pods
sudo kubectl -n appliance-builds logs <pod-name>
sudo journalctl -u k3s -f
```

Useful host-level raw log paths today:

```text
/var/log/containers/
/var/log/pods/
```

The pattern in this document is meant to add an appliance-owned,
operator-friendly log tree on top of that current runtime behavior.

For the services already migrated to the new pattern, the first host paths to
check are:

```text
/var/log/appliance/control-plane/
/var/log/appliance/ui/
```

## Target Layout

### 1. Always-Running Workloads

For long-running product services, use:

```text
/var/log/appliance/<service>/
```

Examples:

```text
/var/log/appliance/control-plane/
/var/log/appliance/ui/
/var/log/appliance/argo-controller/
/var/log/appliance/zot/
```

Expected files inside each workload directory:

```text
stdout.log
stderr.log
startup.log
application.log
```

Optional later files:

```text
audit.log
security.log
requests.log
```

The exact set of application-managed files can grow later, but the directory
shape should stay stable.

### 2. Ephemeral Workflow Pods

For short-lived build and related pods, use a workflow-oriented directory
shape:

```text
/var/log/appliance/builds/<workflow-id>/<pod-name>/
```

Examples:

```text
/var/log/appliance/builds/build-019f.../buildah-main/
/var/log/appliance/builds/build-019f.../syft-scan/
```

Expected files:

```text
stdout.log
stderr.log
startup.log
```

Application-specific log files are not a first requirement for these
ephemeral workflow pods. For the first cut, preserving `stdout`/`stderr`
cleanly is sufficient.

## Core Design Rules

### Rule 1. `stdout` and `stderr` stay intact

The existing container `stdout`/`stderr` behavior remains important and should
continue.

This is needed for:

- `kubectl logs`
- Kubernetes-native debugging
- cluster-level log forwarding later
- early bootstrap and crash diagnostics before file logging is fully ready

The appliance log pattern must not replace `stdout`/`stderr`; it should mirror
them into well-known files.

### Rule 2. Application logs and bootstrap logs are separate concerns

There are two categories of logs:

1. Process `stdout`/`stderr` and startup-time failures
2. Application-logic logs produced intentionally by service code

These should both exist:

- startup script captures `stdout` and `stderr`
- Go code writes structured application logs to specific files using the
  shared logging module

### Rule 3. Service-first operator paths

Operators should always be able to answer:

1. which appliance service is affected
2. which file inside that service contains the needed signal
3. for builds, which workflow or pod instance is affected

Kubernetes namespaces still matter for `kubectl logs`, but they do not need to
be the first organizing concept inside `/var/log/appliance`.

### Rule 4. The appliance owns the path

The log tree under `/var/log/appliance` is part of the appliance operating
contract on the target host.

It should be:

- created by install/upgrade flow
- reused across pod restarts
- collected by future diagnostics/support-bundle flows
- documented as the first operator-visible place to inspect runtime logs

## Container and Startup Pattern

For each long-running deployment we should standardize this image/runtime
pattern.

### Container Image Contract

Each deployment image should contain:

- the Go binary
- a startup script
- the small shell/runtime tools needed by that script, especially `sh` and
  `tee`

Expected entrypoint pattern:

```text
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
```

The script then launches the service binary.

### Startup Script Responsibilities

Each startup script should:

1. determine the target workload log directory
2. create it if needed
3. write a small `startup.log`
4. mirror process `stdout` to `stdout.log`
5. mirror process `stderr` to `stderr.log`
6. still preserve live container `stdout`/`stderr`
7. `exec` the real Go binary as PID 1 behavior requires

Conceptually:

```text
binary stdout -> container stdout + stdout.log
binary stderr -> container stderr + stderr.log
```

The exact shell implementation can be chosen later, but the behavior should
match that contract.

### Fixed Mount Pattern

To make host-visible files appear from inside pods, the runtime still needs a
mount. That mount is an implementation detail, not a user-facing setting.

For v1, use one fixed host path:

```text
/var/log/appliance
```

Mount that fixed base path into the container at the same path:

```text
/var/log/appliance
```

Then let the startup script and service code write into fixed service-specific
subdirectories, for example:

```text
/var/log/appliance/control-plane/
/var/log/appliance/ui/
/var/log/appliance/argo-controller/
/var/log/appliance/builds/
```

This means Helm or the deployment manifests still need to wire the mount, but
they should not expose the path as a configurable knob.

## Application Logging Pattern

The next step after startup-script capture is service-level structured file
logging.

### Logging Module

For application-managed files, use the shared `platformkit/logging`-based
module already used by the control plane.

That keeps:

- format consistency
- redaction behavior
- future rotation hooks
- cross-repo logging behavior aligned

### Service-Level Intent

For the control plane and UI service:

- `stdout`/`stderr` remain for startup/crash/container visibility
- application code writes core logic logs to `application.log`
- later, high-value categories can split into dedicated files only if clearly
  useful

We should avoid inventing too many log files too early. The first code-level
target should be one structured `application.log` per always-running service.

## Phased Implementation Plan

### Phase 0. Documentation and naming

Deliverables:

- this document
- final agreement on the fixed root: `/var/log/appliance`
- final agreement on service-first organization

No runtime behavior changes are required in this phase.

### Phase 1. Structural runtime collection for long-running pods

Scope:

- control plane deployment
- UI deployment
- Argo Workflow Controller deployment

Deliverables:

- startup script for each long-running image
- Containerfile/Dockerfile updates to include the script
- fixed host-path-backed writable mount of `/var/log/appliance` for each
  deployment
- mirrored `stdout.log` and `stderr.log`
- `startup.log`

Acceptance:

- `kubectl logs` still works
- `/var/log/appliance/control-plane/stdout.log` exists
- `/var/log/appliance/ui/stdout.log` exists
- `/var/log/appliance/argo-controller/stdout.log` exists

### Phase 2. Control-plane and UI application file logging

Scope:

- control plane Go code
- UI Go code

Deliverables:

- service code writes structured application logs through the shared logging
  module to `application.log`
- startup capture remains unchanged
- UI service emits redacted structured downstream control-plane request and
  response traces in `application.log` by default, with
  `APPLIANCE_UI_CONTROL_PLANE_TRACE=false` available as a temporary opt-out
- control plane emits redacted structured API request and response traces in
  `application.log` for `/api/v1/*` calls

Acceptance:

- application behavior logs are visible in a stable file
- startup/crash logs are still visible through `stdout.log`/`stderr.log`
- redaction behavior remains intact

### Phase 3. Ephemeral workflow pod capture

Scope:

- build/task workflow pods

Deliverables:

- workflow/pod log directory structure under
  `/var/log/appliance/builds/<workflow-id>/<pod-name>/`
- startup capture for those task containers where practical

Acceptance:

- one build can be correlated to a stable host directory
- pod logs remain available after pod termination until normal cleanup

### Phase 4. Diagnostics and support integration

Scope:

- support-bundle collection
- troubleshooting docs
- operator commands

Deliverables:

- support-bundle includes selected `/var/log/appliance/**` content
- documentation points operators first to `/var/log/appliance`
- release/verify flows know how to surface log-path hints

Acceptance:

- a failed appliance can be debugged without discovering ad hoc paths
- support-bundle includes predictable runtime logs

## Repository Responsibilities

### `appliance-code`

Owns:

- startup scripts shipped in images
- Dockerfile/Containerfile changes
- fixed deployment/chart mount wiring for `/var/log/appliance`
- service code changes for structured file logging
- documentation for runtime topology and operator debugging

### `appliance-release`

Owns:

- installer-side creation and permissioning of `/var/log/appliance`
- target-host documentation
- verification/reporting hints that point operators to the log root

### `appliance-ctl`

Likely follow-on ownership:

- diagnostics/support-bundle collection commands
- future explicit log-export or tail helpers if we choose to add them

## Initial Operator Contract

After Phase 1, the operator contract should be simple:

1. Runtime appliance logs live under `/var/log/appliance`.
2. First choose the service directory.
3. For builds, choose the workflow directory and pod directory.
4. Check:
   - `startup.log`
   - `stdout.log`
   - `stderr.log`
   - `application.log`

Examples:

```text
/var/log/appliance/control-plane/
/var/log/appliance/ui/
/var/log/appliance/argo-controller/
/var/log/appliance/builds/
```

## Deliberate Non-Goals For The First Cut

Do not do these in the first implementation step:

- replace `kubectl logs`
- introduce a cluster-wide log aggregation stack
- create many fine-grained application log files immediately
- redesign audit storage here
- force every ephemeral pod to emit rich application-managed files before the
  long-running services are done

The first objective is a predictable, appliance-owned runtime log tree on the
target host.

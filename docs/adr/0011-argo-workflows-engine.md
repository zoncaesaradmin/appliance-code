# ADR 0011: Argo Workflows Behind The Control Plane

- Status: Accepted with validation gate
- Date: 2026-07-03

## Context

The complete appliance needs a workflow engine for multi-step builds, verification, SBOM generation, vulnerability analysis, publication, and future application workflows. The appliance control plane must remain the only public API and security authority. Argo Workflows provides Kubernetes-native reconciliation, but exposing its API/UI or accepting arbitrary Workflow specifications would create a second authorization surface and an unsafe arbitrary-workload interface.

## Decision

- Deploy one Argo Workflow Controller in the complete v1 appliance. Use a namespace-scoped/managed-namespace configuration: the controller lives in `workflows` and manages appliance-owned Workflows and task pods in `appliance-builds`.
- Install the required cluster-scoped CRDs as a separate versioned release input before the appliance Helm release. The installer owns compatibility checks, upgrade ordering, and rollback tests because normal Helm CRD handling does not provide the required upgrade lifecycle.
- Do not expose Argo Server or the Argo UI. The controller can operate independently; the appliance REST/MCP APIs are the only user-facing workflow surface.
- The control plane uses the Kubernetes API through a typed `WorkflowEngine` adapter to create, get, watch, and terminate only Workflow resources bearing immutable appliance ownership labels. Namespace-limited RBAC also permits read-only task-pod status and logs so appliance APIs can report progress. The control plane cannot create arbitrary pods and does not grant users Kubernetes or Argo credentials.
- Users submit typed appliance requests, never raw Workflow YAML, images, commands, scripts, service accounts, volumes, secrets, node selectors, or security contexts. The server assembles Workflows from a versioned, allowlisted template catalog and validates the rendered object before creation.
- Argo owns operational task scheduling and pod reconciliation. The control plane owns authentication, authorization, quotas, idempotency, durable domain state, audit, cancellation intent, result validation, and artifact acceptance.
- Store durable workflow/build history in control-plane SQLite. Apply bounded Workflow and pod TTLs after terminal-state reconciliation. Do not enable Argo workflow archive, node-status offloading, or an Argo persistence database in v1.
- Use separate least-privilege service accounts for the control plane, Workflow Controller, executor, and each workflow class. Never use the `default` service account. Workflow task pods receive only the permissions and short-lived credentials needed for that workflow.
- Pin controller and executor images by digest. Use non-root/restricted security contexts where compatible, default-deny networking, resource quotas, per-principal concurrency, deadlines, retry ceilings, synchronization limits, bounded logs/results, and orphan cleanup.
- V1 does not publish package profiles: Argo, its controller namespace, and the build namespace are included in the one bundle. The Go server still compiles and runs locally without Kubernetes or Argo; workflow integration belongs to the K3s test lane.

## Failure And Recovery Contract

- A submitted Workflow name is deterministic from the appliance operation ID, making retries idempotent.
- The control plane reconciles SQLite intent against Workflow resources after either process restarts and classifies missing, unknown, terminal, and orphaned resources explicitly.
- Cancellation records intent before terminating the Workflow. A race with successful completion is resolved from observed terminal state and accepted outputs, never from request timing alone.
- Argo unavailability degrades workflow features but does not disable identity, REST/MCP, or registry read/write operations. Readiness reports the workflow dependency separately.
- Completed Workflow deletion never deletes authoritative build records or accepted OCI artifacts.
- Backup captures the K3s resources and release-owned template/configuration versions, but in-flight task execution is not promised to resume after clean-node restore. Reconciliation either resumes a provably safe operation or records an explicit interrupted terminal state; publication steps must be digest-bound and idempotent.

## Consequences

Argo adds an always-running controller pod, CRDs, controller/executor images, RBAC, upgrade compatibility, and another reconciliation loop to package and support. In return, the application does not need to implement DAG scheduling, retries, task dependency handling, and pod lifecycle mechanics itself.

Omitting Argo Server and workflow archive keeps the public attack surface and database footprint small. If future workflows require large-spec offloading, long Argo-native history, or direct operator UI access, that requires a new decision covering persistence, identity integration, ingress, backup, and authorization.

## Verification

- Prove the pinned Argo release against the pinned K3s/Kubernetes release, including CRD install/upgrade/rollback and air-gapped image loading.
- Verify namespace and label isolation: the controller cannot manage unrelated namespaces, and the control plane cannot create arbitrary pods or non-appliance Workflows. Verify that its pod access is read-only and that API log responses enforce appliance ownership and redaction.
- Reject template injection, arbitrary images/commands, forbidden service accounts, secrets, host resources, privilege, mutable refs, and resource-limit escalation.
- Test success, failure, retry, deadline, cancellation races, TTL cleanup, quota/concurrency, bounded output, log redaction, and orphan cleanup.
- Restart the control plane, Workflow Controller, task pods, and node at every workflow phase and prove deterministic reconciliation without duplicate publication.
- Render and install the complete Helm values; prove all Argo resources are bundled, pinned, namespace-restricted, and functional with public egress denied.

## References

- [Argo Workflows architecture](https://argo-workflows.readthedocs.io/en/latest/architecture/)
- [Managed namespace installation](https://argo-workflows.readthedocs.io/en/latest/managed-namespace/)
- [Workflow RBAC](https://argo-workflows.readthedocs.io/en/latest/workflow-rbac/)
- [Workflow executors](https://argo-workflows.readthedocs.io/en/latest/workflow-executors/)

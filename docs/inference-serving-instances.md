# Inference serving instances

## Status and scope

This document is the cross-repository contract for evolving appliance
inference from one active engine into a small, native multi-instance serving
fabric. It deliberately borrows durable concepts from KServe without adopting
KServe, its CRDs, or its advanced scheduling features.

The first implementation remains deliberately constrained: one signed runtime
implementation, one `default` serving instance, and one model in that
instance. The contract makes additional layouts possible only after their
runtime-specific validation is added.

This document does not authorize an alternate runtime image, a public package
repository, or online runtime acquisition. All manager and runtime images stay
signed, bundle-present, and digest-pinned under the existing air-gapped
release contract.

## Alpha serving policy

Alpha intentionally permits multiple **downloaded** models but exactly one
**enabled** model. The manager accepts only the `default` serving instance with
one model and one replica; enabling another downloaded model atomically replaces
the default desired binding. This is a product policy enforced by the manager,
not an accidental property of the current engine Deployment.

The durable model-instance and binding shapes remain plural so a later release
can raise these limits after implementing admission control, per-instance
reconciliation, routing, metrics, and failure isolation. Until then, no API or
UI control may create a second instance, a second enabled model, or a replica
set.

## Concepts

The product uses these terms consistently:

| Term | Meaning |
| --- | --- |
| Model | An imported, verified weight set plus immutable identity and metadata. |
| Runtime | The one signed runtime implementation selected by the appliance package. |
| Serving profile | A reusable resource and placement policy, not an appliance profile. |
| Model instance | Desired execution of a runtime with a model set, serving profile, placement, and replica count. |
| Binding | Mapping from a client-visible OpenAI model name to one or more ready model instances. |

The desired relationship is:

```text
Model -> Model instance -> Runtime -> Placement -> Endpoint
```

An appliance **profile** continues to select product capabilities, including
whether inference is enabled. It must not choose model placement, instance
count, or a runtime-specific serving mode. The signed delivery package selects
the one runtime implementation. A **serving profile** is local desired state
used by the manager/controller to decide resources and placement.

## Runtime capability declaration

The signed runtime definition declares only behavior the controller must know
to admit and reconcile an instance. Initial package metadata has a single
runtime image; the following is the target semantic shape, not a second source
of runtime images:

```yaml
serving:
  modes: [single-model-per-instance]
  maxModelsPerInstance: 1
  supportsReplicas: true
  requiresGPU: false
```

`modes` is an allow-list. A manager must reject an instance whose requested
layout exceeds it. In particular, a runtime must not be treated as
multi-model-capable merely because it can cache multiple models internally.
The runtime package must explicitly declare and test that mode.

## Desired state

A model instance has a stable, appliance-scoped identifier and contains at
least:

```yaml
id: default
runtimeRef: selected-appliance-runtime
servingProfileRef: default
models: [example/model]
replicas: 1
placement:
  class: standard # CPU/GPU policy defined by the serving profile
```

The manager owns the desired state and the observed status. Kubernetes
Deployment, Service, Pod, and PVC names are derived implementation details;
they are never client API identifiers. A model instance owns its ephemeral
runtime cache and serving resources. It does not own model weights.

The initial `default` instance is the successor to the current active model.
Creating or updating it preserves today's Load workflow. Future instances may
be independent, may use the same model, and may have replicas. Multi-model
instances are invalid until the runtime declares that capability.

## Gateway and controller boundary

The inference manager has two small, explicit responsibilities:

1. The **gateway** authenticates through the existing control-plane boundary,
   resolves the requested OpenAI `model` to a binding, selects one ready
   instance, and proxies the request unchanged to that instance.
2. The **controller** reconciles desired model instances into owned Kubernetes
   Deployments and per-instance Services, then reports their observed status.

This is not a generic scheduler and is not a replacement for KServe. It does
not implement prefix-aware routing, disaggregated serving, multi-node model
parallelism, or fine-grained GPU sharing. A future advanced backend may
implement the same model-instance contract, but the appliance keeps the native
Deployment/Service controller for its supported single-node baseline.

Selection occurs before an OpenAI request is streamed and remains fixed for
that request. The gateway never loads, unloads, or switches a model in the
request path.

## API contract

OpenAI-compatible inference remains client-facing and model-based. Clients do
not learn runtime, instance, Kubernetes, or endpoint names. The existing
`/inference/*` public prefix (and configured `/ai/v1/*` alias) remains the
only public inference proxy boundary.

The control-plane lifecycle API evolves to resource-oriented management:

```text
GET/POST          /api/v1/inference/instances
GET/PUT/DELETE    /api/v1/inference/instances/{id}
GET/PUT/DELETE    /api/v1/inference/bindings/{modelAlias}
GET               /api/v1/inference/status
```

The existing model import, inventory, catalog, capability, and delete APIs
remain model-oriented. The existing Load action is redefined as a convenient
operation on `default`: replace its one-model desired state, wait for
readiness, and bind that public model name. The Alpha UI calls this **Enable**
so the replacement behavior is clear, while retaining the existing load
endpoint and workflow.

The initial status response retains the familiar default-model summary and
adds an instances list. A future API version may retire singleton fields only
after all in-product callers use the instance list.

## Admission, storage, and isolation

Before reconciling an instance, the controller must validate the aggregate
commitment:

```text
per-instance model memory + KV/cache + shared memory + runtime margin
multiplied by replicas, plus all ready or rolling-out instance reservations
must not exceed the applicable host or GPU budget.
```

The current per-model planner is the input to this calculation; it is not
discarded. A rejected instance must not disrupt an already ready instance.

Models are imported once into the appliance model store. The manager has the
smallest necessary write access for import and registry metadata. Runtime
instances mount model weights read-only wherever engine behavior permits and
use an instance-qualified writable cache and temporary directory. Instance
logs are stored under `/data/zon/logs/inference/<instance-id>/`; deleting an
instance removes only its serving resources and ephemeral state, never its
model weights. Model deletion fails while a desired or observed instance uses
the model.

Generated workloads preserve Restricted Pod Security Admission, fixed
non-root IDs, digest-pinned images, read-only root filesystems, narrow writable
mounts, NetworkPolicy, and least-privilege manager RBAC. Every controller-owned
resource carries appliance ownership and instance labels.

## Migration and rollout

Upgrade from the current singleton implementation is an explicit convergence
step:

1. Read the durable downloaded-model registry and load progress.
2. If a model was active, synthesize the `default` desired instance and binding.
3. Reconcile and wait for the new instance to become ready.
4. Switch the stable manager gateway to instance routing.
5. Delete the legacy singleton engine only after successful cutover.

If reconciliation fails, retain the previously ready serving path and report a
failed migration; do not silently delete the old engine. Runtime-family changes
remain separately gated by the signed package and supported migration policy.

## Delivery sequence

1. Define the typed model-instance, binding, serving-profile, runtime-capability,
   and status contracts with tests.
2. Refactor the manager internally so `default` implements the current Load
   behavior through those types.
3. Introduce per-instance reconciliation and model-aware gateway routing.
4. Convert Helm and `zonctl` installation/upgrade to deploy the inference
   fabric and perform singleton migration.
5. Update the control plane/UI to manage the default instance, then expose
   advanced instances to administrators.
6. Enable multiple one-model instances, replica sets, and finally declared
   multi-model instances only with the corresponding validation matrix.

Each phase must verify fresh install, existing appliance upgrade, interrupted
reconciliation, manager restart, reboot, offline operation, capacity rejection,
and preservation of a separate ready instance during a failed rollout.

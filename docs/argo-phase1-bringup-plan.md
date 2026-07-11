# Argo Phase 1 Bring-Up Plan

This document captures the first executable Argo integration slice for the
appliance: include the Argo Workflow Controller in the appliance packaging and
bring it up successfully inside K3s before using it for real build workflows.

## Goal

Make Argo part of the appliance bundle and installed topology so that:

- the Argo Workflow Controller runs in `appliance-workflows`
- the managed workload namespace `appliance-builds` exists
- Argo CRDs are recognized as first-class release inputs
- the release/installer path can verify that Argo is present and healthy

This phase does **not** yet switch the control plane from the in-process fake
workflow engine to a real `internal/workflows/argo` adapter. It prepares the
packaging, chart, and verification surface for that later step.

## Scope

Included in this phase:

- Argo chart/module owned in `appliance-code`
- namespace layout for `appliance-workflows` and `appliance-builds`
- first-pass Argo controller Deployment and ServiceAccounts
- first-pass namespace-scoped RBAC and controller configuration wiring
- release-input contract updates for Argo chart, CRDs, and pinned images
- installer/release verification requirements for "controller is running"

Deferred to later phases:

- real control-plane `internal/workflows/argo` implementation
- rootless Buildah workflow tasks
- real build submission through Argo
- workflow TTL/reconciliation validation
- final hardened NetworkPolicy and quota rules, after controller behavior is
  proven against the pinned K3s release

## Namespace Model

Two namespaces are required and intentionally distinct:

- `appliance-workflows`
  - the always-running Argo Workflow Controller
  - controller configuration
  - controller ServiceAccount and leader-election RBAC
- `appliance-builds`
  - appliance-owned `Workflow` objects
  - future task pods such as Buildah, SBOM, scan, and image utility pods
  - workflow/executor RBAC and later quotas/cleanup policy

This keeps the long-lived workflow engine separate from the short-lived
workload pods it reconciles.

## Packaging Contract

The first complete Argo release-input contract should include:

- Argo CRDs as a separate versioned release input
- Argo controller chart/package content
- pinned controller image archive and image reference
- pinned executor image archive and image reference
- compatibility metadata tying the Argo version to the pinned K3s release

The installer must apply Argo CRDs before the appliance Helm release, because
the CRD lifecycle is not delegated to normal Helm templating.

## First Verification Gate

This phase is successful when a clean appliance install can prove:

- `workflows.argoproj.io` CRDs are installed
- `appliance-workflows` namespace exists
- `appliance-builds` namespace exists
- the Workflow Controller Deployment is ready
- the controller pod restarts successfully
- no Argo UI or Argo Server is exposed
- existing control-plane, Traefik, and zot flows still work

The release verification lane should later check at least:

- `kubectl get ns appliance-workflows appliance-builds`
- `kubectl -n appliance-workflows get deploy,pods`
- `kubectl get crd workflows.argoproj.io`

## Execution Order

1. Add the repo-owned Argo chart/module and namespace/RBAC/controller scaffold.
2. Extend release-input metadata to represent Argo chart/CRDs/images.
3. Add installer ordering in `appliance-release` for Argo CRDs before chart
   install.
4. Add target-host verification that the controller is up.
5. Only then begin the real workflow-engine adapter and workflow submission
   work.

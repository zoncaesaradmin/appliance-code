# Appliance Control Plane V1 Plan

## Purpose

This document captures the v1 plan for this repo to become the appliance control plane server. It is based on the shared ChatGPT discussion titled `Appliance Front-facing Interfaces` fetched on July 3, 2026, and turns that summary into an implementation plan for this codebase.

The goal for v1 is a single control plane server that exposes:

- Regular HTTP APIs under `/api/v1`
- An MCP endpoint at `/mcp`
- OCI registry token and lifecycle APIs, with the control plane owning token issuance

The server must support the basic appliance flows for local users, builds, artifacts, and automation, while remaining easy to extend later for external identity providers such as LDAP/AD, SAML, and OIDC.

V1 is an offline-first product with one production package: a complete signed air-gap appliance bundle. Installation and runtime must work with public network egress denied. The bundle contains every product dependency, including supported-host package prerequisites, K3s and its images, the control plane, zot, Argo, OCI task images, scanner data, charts, CRDs, and verification material.

Local development and source-repo validation still remain mandatory. The
control plane must build, run, and pass end-to-end tests on a normal
development machine without K3s, containers, or packaging. The execution
plan for that local-first test lane is in [e2e-testing-plan.md](e2e-testing-plan.md).

## Summary From The Shared Discussion

The discussion converged on a simple v1 identity model:

1. There are only two authentication mechanisms:
   - Username + password for interactive login only
   - API tokens for all automation and machine-facing access
2. Username/password must never be used directly for automation.
3. Interactive login returns a short-lived session token.
4. Long-lived API tokens are created only after interactive login.
5. REST APIs, MCP, CLI, CI/CD, and Podman all converge on the same API token model.
6. OCI image and artifact push/pull uses the standard challenge flow: the control plane validates the API token and issues a short-lived, repository-scoped token that zot verifies.
7. RBAC must apply consistently across REST APIs, MCP, and registry-authenticated operations.
8. Future external auth integration should swap only the login provider, not the token, RBAC, or automation model.

## V1 Product Boundaries

### In Scope

- Single control plane server process
- `/api/v1` HTTP endpoints for auth, users, roles, tokens, builds, and artifacts
- `/mcp` endpoint with authentication, authorization, and protocol shell
- OCI registry token and lifecycle APIs used by Podman, Skopeo, Buildah, Helm, and ORAS clients
- Appliance-profile selection at startup, resolving product-level appliance
  profiles into implementation-level appliance capabilities. See
  [appliance-profiles-v1.md](appliance-profiles-v1.md).
- Local user database
- Role-based access control
- Audit-friendly authentication and authorization flow
- Deployment model for K3s with Traefik ingress

### Out Of Scope For The First Phase

- Full MCP tool implementation
- External AAA integrations such as OIDC, SAML, LDAP, or AD
- Full artifact registry implementation in this repo if a separate OCI registry is used as the data plane
- A heavy SPA-style frontend is out of scope; the product may ship a separate lightweight Go/HTMX UI service while zot's embedded browse/search UI remains an ADR 0008 gated candidate
- Multi-node control-plane HA and multi-site replication

Single-appliance durability, backup, restore, safe upgrade, and disaster recovery are explicitly in scope. A non-HA appliance still needs predictable recovery from process, node, disk, certificate, and upgrade failures.

## Working Decisions (July 3, 2026)

These decisions are now considered the default implementation direction unless we explicitly revise them later.

- Persistence starts with SQLite for v1.
- The control-plane code must stay modular so its persistence can later be backed by Postgres.
- The future control-plane Postgres target is a separate database/schema and credential set in a Postgres service pod running in K3s.
- For the first cut, local and production-oriented development should use the same SQLite-backed implementation.
- The local-versus-production persistence split can be introduced later when we move to Postgres.
- Local development must keep working by building and running the Go server directly on the host machine.
- We may use local-versus-production implementation patterns later where helpful, but not in the first cut for persistence.
- Token revocation and session handling should follow standard server-side security practices.
- zot will be the OCI image and generic OCI/ORAS artifact data plane; v1 does not claim native non-OCI package-manager protocol support.
- V1 security, MCP compatibility, RBAC, HTTP, audit, telemetry, supply-chain, support, and zot-profile defaults are fixed by ADR 0010.
- MCP should exist in v1, but placeholder or unimplemented tool behavior is acceptable.
- API conventions should follow standard patterns rather than inventing custom appliance-only styles.
- The public `appliance-release` repo owns user-facing installation, lifecycle, and bundle distribution; this private repo owns product behavior and signed release inputs.
- There is no connected production package in v1. Updates are complete signed offline bundles, and released components never download or self-update at runtime.

## V1 Architectural Decisions

### 1. Single Control Plane Service

Run one server for:

- REST APIs
- MCP endpoint
- OCI registry token and lifecycle APIs

This keeps the front-door security model unified and lets us apply one authn/authz stack everywhere.

### 2. K3s + Traefik As The Base Deployment Model

Use K3s as the appliance Kubernetes substrate and Traefik as the ingress controller.

The pod, Argo workflow, Job, routing, namespace, network, and storage layout is captured in the [K3s deployment topology](deployment-topology.md).

Deploy a namespace-scoped Argo Workflow Controller behind the control plane as part of the single complete v1 appliance. The control plane submits only appliance-generated `Workflow` resources and remains the sole public API, identity, authorization, audit, and durable-state authority.

Expected front-door routing:

- `/api/v1/*` -> control plane service
- `/mcp` -> control plane service
- `/internal/auth/check` -> control plane service, cluster-internal only, for Traefik ForwardAuth checks on protected application routes
- `/api/v1/registry/*` -> control plane service
- `/v2/*` -> OCI registry service

This repo implements API-token lifecycle, OCI scope authorization, and the OCI registry token-service endpoint. zot implements the OCI data plane and verifies control-plane-signed registry tokens.

### 3. Local Users First, Pluggable Identity Later

V1 should implement only local users, but behind a layered authentication provider interface. The provider boundary must make it possible to add:

- OIDC login
- SAML login
- LDAP/AD-backed authentication
- External group-to-role mapping

without changing the token model or the authorization model.

### 3a. Local-First Binary, Runtime-Selectable Infrastructure

Follow the same high-level discipline visible in sibling repos:

- the server must compile and run directly as a local Go binary
- local development must not require containers or K3s
- production packaging may add containers, Helm, K3s, and managed services around the same binary

Important constraint:

- prefer runtime-selected implementations behind Go interfaces for core control-plane behavior
- use build tags carefully, mainly for optional heavy integrations or developer-only helpers
- the `platformkit` style local-versus-production swap is an acceptable future option
- for the first cut of persistence, avoid splitting behavior and keep one SQLite-backed implementation behind the interface

### 3b. Explicit OCI Toolchain Contract

The authoritative tool ownership and compatibility gates are recorded in [ADR 0009](adr/0009-oci-toolchain.md).

Use one named tool for each responsibility:

- Buildah builds OCI images from `Containerfile` input. A file literally named `Dockerfile` is accepted only as a Buildah-compatible filename alias.
- Podman runs images for local use and disposable runtime smoke tests and acts as a supported registry client.
- Skopeo inspects remote manifests, verifies digests, copies/promotes images, and synchronizes image content for air-gapped workflows.
- ORAS pushes, pulls, attaches, discovers, and copies generic non-image OCI artifacts.
- zot stores and serves OCI images and artifacts.
- Helm consumes and publishes OCI-hosted charts where chart workflows are required.

K3s uses its embedded container runtime internally. That is a K3s implementation detail, not an application-facing toolchain choice, and the control plane must not mount or call its runtime socket.

The Go server should invoke these capabilities only through narrow domain interfaces. In K3s, build and related multi-step operations run as appliance-generated Argo Workflows; direct local adapters may be used only in explicit development/test lanes. It must not shell out to Podman during normal control-plane request handling. Local Go build/test/run remains independent of all OCI tools and Argo; toolchain integration tests are separate, explicitly gated lanes.

### 4. One Automation Credential Type

All machine-facing clients should use one long-lived opaque API token format, for example:

```text
apt_xxxxxxxxxxxxxxxxx
```

The server stores only a hash of the token secret, never the raw token value.

### 5. Consistent RBAC Everywhere

Every protected path must resolve to the same authorization engine:

- REST endpoints
- MCP requests
- OCI registry token and lifecycle actions
- Build-triggered actions

This avoids separate policy islands.

### 6. Security And Recovery Are Release Properties

Security, backup, restore, and upgrade behavior are not a final hardening phase. Each implementation phase must include the controls, failure behavior, tests, and operational documentation needed for the feature introduced in that phase.

The release must be recoverable by an operator who has node access but does not have a working control-plane login. Recovery must not depend on the same component that has failed.

## Core Identity And Security Model

### Authentication Types

#### Interactive Login

- Input: username + password
- Output: short-lived session JWT plus an opaque rotating refresh credential
- Access lifetime: 15 minutes; refresh idle lifetime: 12 hours; absolute session-family lifetime: seven days
- Refresh rotates on every use; reuse revokes the complete family; maximum five concurrent session families per user
- Usage: browser/admin/user-facing actions only

#### API Token

- Created only by an authenticated interactive user
- Used for CLI, automation, REST, MCP compatibility mode, CI/CD, and authenticating to the registry token endpoint
- Long-lived but revocable
- Default lifetime 90 days, maximum 365 days, minimum one hour; no non-expiring v1 tokens
- Stored hashed at rest
- Shown only once at creation time

#### Registry Access Token

- Five-minute JWT minted by the control plane during the OCI registry challenge flow
- Scoped to allowed repository actions after intersecting requested scope with appliance RBAC
- Never used as a general appliance API credential
- Signed with a registry-specific Ed25519 key and verified by zot after the pinned-release compatibility gate passes

### Authorization Model

Implement RBAC with:

- Users
- Roles
- Permissions

Accepted initial permission families:

- `users.read`
- `users.create`
- `users.update`
- `users.disable`
- `roles.read`
- `roles.create`
- `roles.update`
- `roles.delete`
- `tokens.read.self`
- `tokens.create.self`
- `tokens.create.any`
- `tokens.revoke.self`
- `tokens.revoke.any`
- `builds.create`
- `builds.read.self`
- `builds.read.any`
- `builds.cancel.self`
- `builds.cancel.any`
- `artifacts.read`
- `artifacts.delete.self`
- `artifacts.delete.any`
- `operations.read.self`
- `operations.read.any`
- `registry.pull`
- `registry.push`
- `registry.delete`
- `registry.grants.read`
- `registry.grants.write`
- `mcp.invoke`
- `system.read`
- `system.operate`
- `audit.read`
- `audit.export`

Accepted built-in roles:

- `administrator`
- `developer`
- `viewer`
- `automation`

V1 supports immutable built-in roles plus custom roles assembled only from the published permission catalog. Built-in role names and IDs are stable; their effective permission changes are versioned and documented. The last-administrator invariant applies to custom-role changes as well as user changes.

Role assignments and ownership behavior are fixed in [ADR 0010](adr/0010-v1-security-and-operations-defaults.md). V1 uses appliance-wide roles with own-resource checks for builds, tokens, and build-produced artifacts, plus simple user/role repository-prefix grants. A full project model is deferred.

### Security Requirements

- Password hashing with `argon2id`
- Opaque API tokens with server-side hashing
- JWT signing key rotation support
- Secure token expiry validation
- Authentication and authorization audit logs
- Rate limiting on login and token-sensitive endpoints
- Account disable support
- Password reset flow for admins
- No plaintext secrets in logs
- TLS termination at Traefik
- Cluster-internal HTTP protected by default-deny NetworkPolicy; internal mTLS is deferred from v1

## Required HTTP And MCP Surface

### Authentication

```text
POST /api/v1/auth/login
POST /api/v1/auth/logout
POST /api/v1/auth/refresh
POST /api/v1/auth/password/reset
GET  /api/v1/auth/session
```

### User Management

```text
POST   /api/v1/users
GET    /api/v1/users
GET    /api/v1/users/{id}
PATCH  /api/v1/users/{id}
POST   /api/v1/users/{id}/disable
POST   /api/v1/users/{id}/enable
POST   /api/v1/users/{id}/unlock
POST   /api/v1/users/{id}/password-reset
PUT    /api/v1/users/{id}/roles
```

V1 should disable users rather than physically delete security principals. Login usernames are immutable; `PATCH` may change presentation metadata and allowed account attributes only. Historical audit and build records must continue to resolve the original actor. The service must reject any operation that would leave the appliance without an enabled effective administrator.

### Role Management

```text
GET    /api/v1/roles
GET    /api/v1/permissions
POST   /api/v1/roles
PUT    /api/v1/roles/{id}
DELETE /api/v1/roles/{id}
```

### API Token Management

```text
POST   /api/v1/tokens
GET    /api/v1/tokens
DELETE /api/v1/tokens/{id}
POST   /api/v1/users/{userId}/tokens
DELETE /api/v1/users/{userId}/tokens/{tokenId}
```

### Registry Authentication And Integration

```text
GET /api/v1/registry/token
GET /api/v1/registry/repositories
GET /api/v1/registry/repositories/{name}/tags
GET /api/v1/registry/repositories/{name}/referrers
GET /api/v1/registry/grants
POST /api/v1/registry/grants
DELETE /api/v1/registry/grants/{id}
```

The token endpoint follows the OCI Distribution token-service contract and is called automatically by Podman, Skopeo, Buildah, Helm, and ORAS after the registry challenge. Login uses the appliance username plus an appliance API token, never an interactive password; the endpoint verifies token ownership, parses and intersects scope with current RBAC, and returns no registry refresh token in v1. Repository, tag, and referrer endpoints are appliance APIs backed by the zot adapter and appliance metadata; payload transfer remains on `/v2/*`.

### Build APIs

```text
POST   /api/v1/builds
GET    /api/v1/builds
GET    /api/v1/builds/{id}
POST   /api/v1/builds/{id}/cancel
GET    /api/v1/builds/{id}/logs
```

Build creation accepts an idempotency key. Cancellation is an auditable state transition; record retention and deletion are separate administrative concerns.

### Artifact APIs

```text
GET    /api/v1/artifacts
GET    /api/v1/artifacts/{id}
DELETE /api/v1/artifacts/{id}
```

### Audit APIs

```text
GET  /api/v1/audit/events
POST /api/v1/audit/exports
GET  /api/v1/audit/exports/{id}
GET  /api/v1/audit/exports/{id}/content
```

An administrator-created password reset returns one opaque 256-bit reset credential once. Store only its keyed digest; expire it after 15 minutes; allow one use; and revoke the user's sessions and API tokens when the new password commits. Audit exports are asynchronous, bounded, integrity-checkpointed operations and never contain raw credentials.

### MCP

```text
POST /mcp
```

For the MCP protocol phase, this endpoint should:

- Authenticate API tokens
- Run RBAC checks after protocol method validation
- Implement the pinned MCP Streamable HTTP protocol revision and initialization contract
- Advertise an empty tool set when tools are not enabled
- Return standard JSON-RPC/MCP errors for unsupported methods or unavailable capabilities

Any additional `GET` or `DELETE` behavior required by the pinned MCP protocol mode must live on the same `/mcp` endpoint. Protocol health belongs on appliance health endpoints, not in a private MCP extension.

### Operations And Discovery

```text
GET /api/v1/operations/{id}
GET /health/live
GET /health/ready
GET /health/startup
GET /api/v1/system/version
GET /api/v1/system/status
```

Health endpoints are served on an internal listener or restricted route and return minimal information. Detailed status and version/compatibility data require operator authorization.

V1 does not serve OAuth protected-resource metadata. That route is added only with the future MCP OAuth standards-mode adapter.

Artifact deletion, audit export, restore preparation, and other non-immediate actions return `202 Accepted` with a durable operation ID and `Location`. Operation state is monotonic, owner/RBAC filtered, idempotent where requested, restart-reconciled, and uses problem details for terminal failures.

## Basic End-To-End Flows The Repo Must Support

### Bootstrap

Installer or bootstrap job provides:

- appliance hostname
- initial admin username
- initial admin password

The system creates:

- local admin user
- `administrator` role binding

### Interactive Admin Flow

1. Admin logs in with username and password.
2. Server returns a short-lived session token.
3. Admin creates users and assigns roles.

### Developer Automation Flow

1. Developer logs in interactively.
2. Developer creates an API token.
3. Developer uses the API token for REST, MCP, CLI, and CI/CD.

### Registry Push/Pull Flow

1. Developer or build system runs `podman login`.
2. zot challenges the client with the control-plane token-service realm, service, and requested repository scope.
3. The client presents the appliance API token to the control-plane token endpoint.
4. The control plane authenticates the token, evaluates current RBAC, and signs only the allowed repository actions.
5. zot validates the short-lived token and accepts or rejects the operation.
6. User disable or API-token revocation prevents new registry tokens immediately; existing access expires within five minutes.

### Build Flow

1. Client calls `POST /api/v1/builds` with an API token.
2. Control plane authenticates and authorizes the request.
3. Build service renders a versioned, allowlisted workflow template and creates an Argo `Workflow` through the Kubernetes API.
4. Argo Workflow Controller reconciles the workflow into isolated Buildah, verification, SBOM, and scan task pods as required.
5. The control plane reconciles Workflow status into its durable build state and exposes status/logs/cancellation through appliance APIs.

## Proposed Repo Structure

This repo is not a single-service codebase: it is the product repo for the appliance's control-plane service today, with room for additional independently versioned services (and their own client SDKs) as siblings, the same pattern used in this org's other multi-service repos. Each functional area below is its own Go module with its own `go.mod` and `Makefile`; the repo root holds no `go.mod` and its `Makefile` only delegates (`$(MAKE) -C <module> <target>`) to per-module targets plus a small number of repo-wide composite targets (`build`, `test`, `lint`, `coverage`, `verify`, `run`, `stop`, `dev-k3s`, `clean`). A root-level `go.work` ties the modules together for local development, including the shared `../platformkit` module.

```text
services/
  controlplane/            -- the control-plane service (this phase's implementation)
    cmd/
      appliance-server/
        main.go
    internal/
      app/
      config/
      httpapi/
      mcp/
      authn/
      authz/
      users/
      roles/
      tokens/
      registryauth/
      zotadapter/
      workflows/
        argo/
      builds/
        buildah/
      images/
        skopeo/
      artifacts/
        oras/
      scanning/
        grype/
      sbom/
        syft/
      kube/
      storage/
      audit/
      maintenance/
      observability/
    go.mod
    Makefile
sdk/
  golang/
    applianceclient/       -- Go client SDK for the control-plane REST API
      go.mod
      Makefile

deploy/
  charts/
    appliance-control-plane/ -- the control-plane's own Helm chart, its own Go module
      go.mod
      Makefile
  k3s/
  traefik/
  manifests/

docs/
  control-plane-v1-plan.md

go.work
Makefile
```

Future services (for example a future frontend, worker, or additional appliance component) are added under `services/`, each with its own module, `Makefile`, and — if it exposes an API other components consume — its own SDK under `sdk/`. A top-level `e2etests/` module is the natural place for cross-service tests once a second service exists; it is not scaffolded yet because there is nothing to cross-test.

Recommended layering within `services/controlplane/internal`:

- `httpapi/` contains REST routing, request parsing, response formatting, and middleware
- `mcp/` contains MCP transport and request handling shell
- `authn/` owns local login, session JWTs, API token verification, and provider interfaces
- `authz/` owns RBAC policy evaluation
- feature packages own business logic
- `workflows/argo/` owns constrained Workflow rendering, submission, observation, termination, status translation, and TTL behavior; it does not own business authorization or durable build state
- `builds/buildah/` owns Buildah task specifications, result parsing, and cleanup but not build authorization, Argo reconciliation, or lifecycle policy
- `images/skopeo/` owns image inspection/copy/promotion command contracts and result translation
- `artifacts/oras/` owns generic OCI artifact media-type/referrer operations and result translation
- `scanning/grype/` owns vulnerability scan Job contracts, database-bundle identity, and normalized finding translation
- `sbom/syft/` owns SBOM generation and CycloneDX/SPDX result translation
- `zotadapter/` owns zot API, health, event, and reconciliation details but not identity or RBAC
- `maintenance/` owns durable scheduling/checkpoints and invokes feature reconciliation interfaces without importing transport handlers
- `storage/` owns repository interfaces and concrete persistence

Interface ownership rules:

- `workflows` defines a workflow-engine contract in domain terms; `workflows/argo` implements it with appliance-owned Argo `Workflow` resources without leaking arbitrary YAML into handlers.
- `builds` defines the `Builder` contract and build result model; `builds/buildah` supplies typed workflow tasks composed by the workflow service.
- `images` defines inspect, copy/promote, and verify contracts; `images/skopeo` implements them without leaking transport-specific command syntax into handlers.
- `artifacts` defines typed artifact/referrer contracts; `artifacts/oras` implements them.
- `scanning` defines scanner inputs, normalized findings, and database-version evidence; `scanning/grype` implements them.
- `sbom` defines source identity and normalized SBOM outputs; `sbom/syft` implements them.
- `zotadapter` implements catalog/data-plane reconciliation contracts defined by `artifacts`, not the reverse.
- A shared hardened process/Job command layer handles bounded output, cancellation, exit classification, credential-file mounting, redaction, and cleanup. Feature handlers never construct arbitrary shell strings.
- Domain interfaces accept structured values such as repository, digest, media type, and policy. They do not accept free-form command arguments.

## Data Model Plan

Minimum v1 entities:

- `users`
- `password_credentials`
- `password_reset_credentials`
- `roles`
- `permissions`
- `user_roles`
- `api_tokens`
- `api_token_scopes`
- `session_families`
- `refresh_credentials`
- `auth_throttle_state`
- `builds`
- `artifacts`
- `registry_grants`
- `audit_events`
- `audit_checkpoints`
- `audit_exports`
- `idempotency_records`
- `maintenance_checkpoints`
- `operations`
- `schema_migrations`

Recommended design choices:

- Keep token metadata separate from token secret material
- Store API token hashes with prefix, owner, created time, expiry, last-used time, revoked time
- Separate auth provider identity from local user profile so external IdP linking can be added later

## Production-Readiness Review Findings

This review treats the appliance as an untrusted-client or enterprise-network-facing security boundary and the build subsystem as potentially hostile. Public internet access is neither required nor trusted. The findings below are ordered by severity. A `P0` item must be resolved before its affected implementation begins. A `P1` item must be implemented before a production release. A `P2` item may be staged, but its extension point and ownership must be preserved.

The P0 architecture decisions were accepted on July 3, 2026 and are tracked in the [decision register](decision-register.md). Decisions marked with a validation gate still require the named compatibility spike before implementation starts.

### P0 Decision: Dedicated Product-Managed K3s Appliance

V1 installs a dedicated single-node K3s appliance. The release owns the pinned K3s configuration and upgrades, while the operator owns the supported host OS, storage device, network, DNS, NTP, and backup destination. See [ADR 0001](adr/0001-dedicated-k3s-appliance.md).

Implementation must enforce:

- Production support starts with Ubuntu Server 24.04 LTS, `linux/amd64`, cgroup v2, and an `ext4` data filesystem.
- The installer installs bundled pinned K3s, creates appliance users/directories/systemd units, and preloads bundled OCI images after explicit confirmation.
- The installer checks but does not silently change host firewall, OS patching, or unrelated security policy. Missing prerequisites fail with actionable instructions.
- Install, repair, reconfigure, hostname/IP change, upgrade, rollback, uninstall-preserving-data, factory-reset, and decommission/secure-delete are separate explicit operations.
- Coexistence with unrelated Kubernetes workloads is unsupported in v1.

The installer must inventory and label everything it owns and must never delete pre-existing host or cluster state during uninstall or rollback.

### P0 Decision: zot With Control-Plane Token Auth

Use a pinned zot release under Apache-2.0. zot is an unprivileged OCI image and artifact data plane with filesystem storage; the control plane owns the token realm, authentication, authorization, signing keys, policy, and artifact lifecycle intent. See [ADR 0008](adr/0008-zot-oci-artifact-registry.md). ADR 0007 and ADR 0002 are superseded.

The product and licensing comparison is captured in [Registry Options Review](registry-options.md).

Required conformance spike:

- Pin the full zot image by digest, prove the exact ADR 0010 enabled/disabled extension set, and verify license notices, SBOM, provenance, and supported upgrade path.
- Prove Podman, Skopeo, Buildah, Helm, and ORAS login/pull/push/inspect/copy/attach/discover, denied scope, malformed scope, token expiry, user disable, and API-token revocation.
- Verify EdDSA compatibility and algorithm allowlisting, issuer, service/audience, `kid`, subject, time claims, repository/action enforcement, and a local-only key-rotation procedure.
- Prove filesystem storage restart, interrupted and concurrent operations, disk/inode exhaustion, scrub, dedupe, garbage collection, metadata rebuild, air-gap behavior, and clean-node restore.
- Prove UI/search authorization filtering and prevent public access to management, metrics, profiling, and debug endpoints for every enabled extension.
- Run the applicable OCI Distribution conformance suite.

Exit criterion: zot scores at least 4.0/5 under the evidence-based rating and all ADR 0008 conformance gates pass against the pinned image, extension set, ingress, and storage configuration.

### P0 Decision: Argo Workflows Behind The Control Plane

Use Argo Workflows as the workflow engine in the complete v1 appliance. See [ADR 0011](adr/0011-argo-workflows-engine.md).

- Deploy one namespace-scoped Workflow Controller and manage workflows only in the dedicated build namespace.
- Do not expose Argo Server/UI through Traefik and do not create a second user identity or authorization path.
- The control plane creates, reads, watches, and terminates only appliance-labeled Workflows assembled from versioned templates, with read-only access to their task-pod status/logs; users cannot submit raw Workflow YAML or arbitrary images/commands.
- Argo is operational state, not durable product state. Persist build/workflow intent, transitions, ownership, results, and audit in SQLite; apply Workflow and pod TTLs after reconciliation.
- Do not enable workflow archive/offloading in v1 and do not introduce PostgreSQL solely for Argo. Reassess archive storage when PostgreSQL becomes the appliance database or retention requirements justify it.
- Package CRDs with explicit ownership and upgrade ordering. Pin controller/executor images by digest and validate Argo/K3s compatibility, RBAC, cancellation, restart reconciliation, and air-gap behavior.
- The single v1 appliance bundle includes Argo and the build namespace. Modular chart boundaries remain, but release-bundle profile selection is deferred.

### P0 Decision: Trusted Builds With Ephemeral Rootless Buildah

A build request can execute attacker-controlled source code. A Kubernetes Job is an execution mechanism, not a security boundary by itself.

Accepted v1 boundary:

- Builds are available only to trusted developers and automation identities; v1 does not claim hostile tenant isolation.
- Use one appliance-generated Argo Workflow per build with ephemeral, rootless Buildah task pods and never mount a host runtime socket.
- Accept `Containerfile` builds from allowlisted internal HTTPS Git sources at immutable commit SHAs and publish OCI output to zot. A file literally named `Dockerfile` is accepted only as a Buildah-compatible filename alias.
- Isolate any required builder security-profile exception to the build namespace. See [ADR 0003](adr/0003-trusted-build-boundary.md).

Minimum controls:

- Dedicated workflow-controller and build namespaces with narrowly scoped service accounts; never mount the control-plane service-account token into build pods.
- Pod Security Admission policy, non-root execution, dropped capabilities, and no host namespaces or host paths. Use `RuntimeDefault` seccomp except for the narrow builder exception proven and documented by ADR 0003's validation gate.
- Default-deny ingress and egress policy with explicit internal DNS, source, and zot allowances. Public egress is denied.
- ResourceQuota, LimitRange, per-user concurrency limits, deadlines, pod/job TTL, cancellation, and orphan cleanup.
- Per-build short-lived credentials, never appliance-wide or long-lived secrets; redact them from specs, events, logs, and support bundles.
- Immutable base and builder image digests, approved Buildah images, Skopeo output verification, provenance, and an auditable link between requester, source revision, build definition, and produced artifact.

If the pinned Buildah configuration requires a privileged pod or host runtime socket, reject it and reopen ADR 0003. Trusted input does not justify granting node-equivalent privilege.

### P0 Decision: Single-Replica SQLite Control Plane

SQLite is acceptable for the first cut, but it constrains topology and storage.

Use the same SQLite adapter locally and in the first appliance release, with one replica and a local `ext4`-backed RWO volume. See [ADR 0004](adr/0004-control-plane-sqlite.md).

Required contract:

- Exactly one control-plane replica while SQLite is active.
- One durable ReadWriteOnce volume on a supported local filesystem; do not place the database on NFS or an unvalidated network filesystem.
- Use WAL mode only after validating the volume semantics; configure foreign keys, busy timeout, transaction discipline, and bounded connection counts explicitly.
- Run schema migrations under an exclusive application-level migration lock and refuse startup on unknown or partially applied versions.
- Use the SQLite online backup API or an equivalent transactionally consistent method; never copy a live database file casually.
- Define corruption detection, startup recovery behavior, disk-full behavior, and restore verification.
- Keep database, signing keys, and required configuration in the same backup compatibility contract.

The future Postgres adapter does not remove the need for a separately planned data migration and rollback strategy. Interface compatibility is not data-format compatibility.

### P0 Decision: Purpose-Separated Installer-Owned Secrets

The installer owns generation and lifecycle wiring for purpose-separated secrets; the operator owns protected backup recovery material and production certificate choice. See [ADR 0005](adr/0005-secrets-keys-and-tls.md).

Before identity implementation, enforce:

- Separate keys or credentials for session signing, registry-token signing, API-token digesting, refresh/reset-credential digesting, cursor integrity, audit-checkpoint signing, backup encryption, TLS, and bootstrap; no reuse across purposes.
- Generate keys from the operating-system CSPRNG, mount them as purpose-specific files, restrict RBAC/filesystem permissions, and include required recovery material in encrypted backups.
- Use Ed25519 session JWTs with issuer, audience, `kid`, a 15-minute lifetime, at most 60 seconds clock skew, and one-token-lifetime rotation overlap.
- TLS modes: operator-supplied certificate or installer-generated appliance CA/leaf certificate. ACME is deferred from v1.
- Hostname/SAN changes, trust distribution, certificate renewal alerts, and behavior when time or certificates are invalid.
- Kubernetes Secret protection, including restricted RBAC and encryption-at-rest posture for the K3s datastore where feasible.

No secret value may be accepted on a command line when a file descriptor, password file, or interactive input is possible, because command lines can leak through process inspection and shell history.

### P0 Decision: Off-Appliance Recovery Sets And Restore-Based Rollback

Backup is not complete until restore has been automated and tested.

Use daily coordinated, encrypted, off-appliance recovery sets with initial RPO 24 hours and RTO 4 hours. K3s uses single-node embedded etcd snapshots; upgrades support N-1 to N, and rollback after state migration restores the complete pre-upgrade set. See [ADR 0006](adr/0006-backup-upgrade-and-recovery.md).

Required release contract:

- State inventory: control-plane SQLite and keys, K3s state/token, zot storage root and extension state, chart values, certificates, and appliance configuration.
- K3s recovery and release inputs must preserve Argo CRDs/configuration, pinned workflow templates, controller settings, and enough operation identity to reconcile or safely fail any workflow that was in flight at backup time.
- Stated RPO and RTO targets, backup schedule, retention, destination, encryption, integrity checks, and free-space thresholds.
- Coordinated zot storage snapshot procedure with tested GC/dedupe/scrub quiescence and documented consistency assumptions.
- Restore onto a clean replacement node, not only in-place restore.
- Pre-upgrade compatibility check, automatic pre-upgrade backup, pinned component versions, migration dry-run where possible, and post-upgrade smoke checks.
- Explicit rollback window. Destructive or backward-incompatible migrations require an expand/migrate/contract sequence or a declared restore-based rollback.
- Automated restore drill and upgrade-from-previous-supported-version test as release gates.

### P1: Harden The Kubernetes And Network Boundary

The chart and manifests must provide secure defaults rather than relying on installer prose:

- Dedicated namespaces and service accounts with least-privilege Role/RoleBinding objects; no `cluster-admin` for the server or workflow engine. The server may mutate appliance-owned Workflow CRs and read their task-pod status/logs, but cannot create arbitrary pods.
- Pod Security Admission labels and restricted security contexts for control-plane and supporting workloads.
- Default-deny NetworkPolicies with explicit flows among Traefik, control plane, DNS, Kubernetes API, Argo Workflow Controller, zot, and workflow task pods. Confirm that the selected K3s CNI/network-policy controller enforces them.
- Resource requests/limits, priority where justified, disruption behavior, topology assumptions, and ephemeral-storage limits.
- `readOnlyRootFilesystem`, non-root UID/GID, dropped capabilities, `allowPrivilegeEscalation: false`, and `automountServiceAccountToken: false` unless a pod genuinely needs Kubernetes API access.
- Pin every image by digest in the air-gap bundle. Define offline image preload, internal DNS/NTP, IPv4/IPv6 stance, and behavior with all public egress denied.
- Do not expose the K3s API, registry administration, metrics, debug, or health internals through the public ingress.

### P1: Complete The HTTP Security Contract

Before handlers are added, publish OpenAPI and enforce the ADR 0010 defaults for:

- Maximum header/body sizes, read-header/read/write/idle timeouts, decompression limits, pagination caps, and graceful shutdown drain time.
- Trusted proxy configuration. Honor forwarded headers only from known Traefik addresses; validate allowed hosts and derive external URLs from configured canonical origins.
- CORS deny-by-default. If browser cookie authentication is introduced, use `Secure`, `HttpOnly`, and appropriate `SameSite` cookies plus CSRF protection.
- `Cache-Control: no-store` for credential-bearing responses, strict content types, `nosniff`, frame restrictions, and a deliberate HSTS policy at ingress.
- Stable error codes without account enumeration or internal details; request IDs must be generated or validated at the trust boundary.
- Idempotency keys for build and other expensive creates, optimistic concurrency or ETags for mutable resources, and deterministic `409`/`412` behavior.
- Soft-disable users rather than destructive deletion. Prevent disabling/deleting the last effective administrator.
- Build cancellation as an explicit state transition, not overloaded record deletion.

### P1: Make Authentication Semantics Precise

Use a short-lived signed access token plus an opaque, rotating, server-stored refresh credential. Store refresh and API credentials as keyed hashes or hashes of high-entropy secrets, expose only a lookup prefix, compare in constant time, and show raw values once.

Implement and test the ADR 0010 defaults for:

- The accepted 15-minute access, 12-hour refresh idle, seven-day absolute, five-session concurrency, refresh-reuse, and 60-second clock-skew behavior.
- Immediate effective revocation on user disable and credential-version change.
- Password policy, breached/default password handling, lockout or progressive delay, reset semantics, username normalization, and non-enumerating login responses.
- Token scopes versus role-derived permissions, expiration defaults, maximum lifetime policy, last-used update batching, and a token inventory that never exposes hashes.
- Authentication precedence when multiple credential types are supplied; ambiguous credentials must be rejected.

Rate limits must be keyed by both account and source and must behave correctly behind Traefik. In-memory-only limits reset on restart and need to be documented as a v1 limitation or persisted.

### P1: Implement MCP As A Protocol Endpoint, Not A JSON Route

For the pinned MCP protocol revision, implement Streamable HTTP requirements including JSON-RPC validation, initialization/capability negotiation, protocol-version handling, content negotiation, request/notification semantics, and standard error behavior. A valid `tools/list` may return an empty list; unknown or unavailable tool calls should return protocol errors rather than an appliance-specific pseudo-protocol.

ADR 0010 selects v1 compatibility mode: clients send an appliance API token in `Authorization: Bearer`, with known-client interoperability tests and an explicit statement that v1 does not implement the MCP OAuth authorization profile. Do not publish OAuth protected-resource metadata in this mode. A future standards mode adds a separate OAuth resource-server adapter and authorization-server discovery while mapping resulting identities into the same appliance RBAC engine.

Also validate `Origin` where present, reject untrusted hosts, bind local-development listeners to loopback by default, and place independent request, session, and concurrency limits on `/mcp`.

### P1: Define Health, Dependency, And Failure Semantics

Provide separate internal endpoints:

- Liveness proves only that the process event loop is healthy; it must not restart the process because zot or another dependency is unavailable.
- Readiness proves the server can safely accept its owned traffic, including writable storage and completed migrations.
- Startup allows migrations and recovery without premature restart loops.

Expose dependency status to authenticated operators, not the public health endpoint. Define degraded behavior independently for SQLite, Kubernetes API, zot, DNS, disk pressure, and clock/certificate problems. Use bounded retries with jitter and circuit breaking; never retry non-idempotent operations blindly.

### P1: Make Audit Logs Durable And Useful

Define a versioned event schema containing event ID, time, actor, authentication method and credential ID, action, target, outcome, reason code, request/correlation ID, and trusted source information. Never record passwords, raw tokens, authorization headers, build secrets, or sensitive request bodies.

Audit writes for security-critical mutations must be transactionally coupled to state changes where practical. Define behavior if the audit sink is unavailable, retention and rotation, operator export, clock assumptions, and tamper-evidence expectations. Ordinary application logs are not the audit record.

### P1: Add Appliance Operability And Supply-Chain Gates

The product needs:

- Structured logs, bounded log rotation, Prometheus-format metrics on an internal listener, documented alerts, and a redacted support-bundle command.
- Capacity checks and alerts for disk, inode, memory, database size, registry storage growth, certificate expiry, backup age/failure, queue depth, and stuck builds.
- Reproducible version metadata covering server, schema, chart, K3s, zot image/extension configuration, and builder compatibility.
- SBOMs, dependency and image vulnerability scanning, license inventory, signed images/release manifests, checksums, immutable version pins, and provenance for release artifacts.
- Outbound telemetry disabled by default, internal-only metrics, and explicit operator-controlled support-bundle export as required by ADR 0010.
- Documented repair, reconfigure, power-loss recovery, uninstall-preserving-data, factory-reset, and decommission workflows with explicit confirmation and backup checks.

### P2: Preserve Future Extension Boundaries

- Identity providers should return a canonical subject plus attributes; external group mapping belongs in a separate mapper, not the provider or RBAC evaluator.
- Feature modules need explicit enablement, dependency declarations, health, permissions, migrations, routes, and reconciliation hooks.
- Avoid a second artifact metadata source of truth. zot is authoritative for manifests, tags, digests, referrers, and blobs; the control plane owns appliance-specific metadata and reconciles events and indexes.
- Use the accepted durable asynchronous operation model for long-running deletes, audit exports, repository maintenance, and recovery preparation; builds retain their richer domain state machine.

## Resolved Defaults And Validation Gates

The architecture and default policies below are settled. Implementation begins with the named validation evidence; failed validation reopens the relevant ADR rather than triggering an undocumented fallback.

### 1. Persistence Model

ADR 0004 selects SQLite behind repository interfaces for local development and the first appliance release.

Options:

- SQLite for the smallest appliance footprint and easiest bootstrap
- Postgres for the later production target
- local file-based storage only as a future option if we later choose a local-versus-production split

Accepted implementation:

- Put all persistence behind repository interfaces from the start.
- Use one SQLite-backed implementation in the first cut.
- Keep the implementation in its own package so a Postgres-backed implementation can be added later.
- Do not let Kubernetes itself become the source of truth for users, tokens, or RBAC state.

Execution notes:

- Avoid an ORM-heavy design that leaks database semantics into handlers.
- Keep SQL and migrations explicit and first-class.
- Keep storage package boundaries narrow: user store, token store, role store, build store, audit store.
- Use a Go storage interface boundary with one SQLite-backed implementation first and one Postgres-backed implementation later.
- Do not spend effort on simultaneous mixed-backend runtime support in one deployment.
- Keep the SQLite implementation usable for both local host execution and early appliance-oriented development.

Architectural boundary:

- use the SQLite implementation for both local development and the first cut of the product
- keep the repository/service interfaces stable so handlers and business logic do not care which implementation is active
- introduce a local file-based implementation only later if we decide the developer workflow truly needs it

Why:

- it keeps the first implementation simpler
- it preserves relational behavior across local and early production-oriented testing
- it still leaves clean room for a later Postgres implementation behind the same interfaces

Tradeoff:

- we still have a future migration from SQLite to Postgres
- but we avoid introducing two persistence behaviors in the first cut

### 2. Session And Token Revocation Semantics

ADR 0010 defines revocation and lifetime behavior precisely.

Accepted behavior from ADR 0010:

- Logout revokes the current server-side session family.
- Password reset and user disable immediately revoke every interactive session; user disable also makes owned API tokens unusable.
- Refresh-token reuse revokes the complete family and emits a high-severity audit event.
- API-token `last_used_at` is batched asynchronously and becomes durable within five minutes.
- Session access is 15 minutes; refresh idle/absolute limits are 12 hours/seven days; API tokens default to 90 days and never exceed 365 days.

Implementation baseline:

- Session JWTs should be short-lived and refreshable.
- API tokens should be opaque, hashed at rest, individually revocable, and optionally expirable.
- Resetting a password should invalidate all interactive sessions.
- Disabling a user should invalidate both sessions and API tokens immediately.
- Sensitive auth endpoints should be rate-limited and audit-logged.

### 3. Bootstrap And Break-Glass Recovery

The installer invokes a node-local `bootstrap init` command with username and password supplied through protected files. It succeeds only when no user exists, writes a consumed marker transactionally with the first administrator, expires unused bootstrap material after 15 minutes, and cannot be reached through public HTTP.

`recovery reset-password` requires root-equivalent node access, an exclusive application lock, a protected password file or interactive input, and explicit confirmation. It revokes the user's sessions and API tokens, preserves the last-administrator invariant, writes a high-sensitivity audit event plus a node-local recovery log, and never re-enables remote bootstrap mode.

### 4. Registry Boundary

The accepted registry architecture is defined in [ADR 0008](adr/0008-zot-oci-artifact-registry.md): zot is the OCI image and generic OCI/ORAS artifact data plane, while the control plane owns the OCI registry token service, API-token authentication, scope authorization, signing, revocation, repository policy, and audit.

Implications for v1:

- zot runs as an unprivileged, single-replica pod with a filesystem PVC and no separate database or identity store.
- V1 supports OCI images, Helm charts, and generic OCI/ORAS artifacts only.
- Repository path prefixes map into the appliance authorization model; anonymous access is disabled.
- This repo owns the appliance control plane, identity, RBAC, and registry-facing access policy.
- zot verifies five-minute tokens signed with a registry-specific Ed25519 key after pinned-release compatibility is proven.
- `/v2/*` is the data path; `/api/v1/registry/token` is the token realm.
- zot is authoritative for OCI manifests, tags, digests, referrers, and blobs; the control plane stores appliance-specific metadata and reconciles events and extension indexes.
- Podman, Skopeo, Buildah, Helm, ORAS, and `zonctl artifact` use `/v2/*`; the control plane never proxies payload bytes.
- ADR 0010 selects full zot with enhanced search, scrub, internal metrics, and internal events. UI and management routes are disabled; search must filter unauthorized repositories and all internal extension routes remain off public ingress.

Design direction from the shared `Lightweight Artifactory Setup` discussion:

- Keep a small appliance control plane even for artifact-oriented deployment.
- The control plane should own users, credentials, RBAC intent, TLS, lifecycle operations, and backup/restore concerns.
- The artifact service should be treated as a data plane behind that control plane, not as the primary system of record for identity.
- Prefer standard OCI token authentication over managed `htpasswd`, because token scopes provide repository-specific pull/push authorization without duplicating users in the registry.

### 5. MCP Transport Scope For V1

Pin one published MCP protocol revision and implement its Streamable HTTP server requirements at `/mcp`. Do not invent a smaller JSON-over-HTTP protocol and call it MCP.

V1 capability scope:

- Implement protocol initialization/version negotiation and required request/notification behavior.
- Advertise no tools and return an empty `tools/list` until tool modules are enabled.
- Do not enable server-to-client requests, resource subscriptions, resumability, or long-lived server state unless a concrete v1 client flow requires them.
- Implement any HTTP methods, headers, content negotiation, session behavior, and errors required by the pinned revision even if most traffic is synchronous `POST`.
- Test with intended real clients and protocol conformance fixtures.

Authorization is appliance API-token Bearer compatibility mode in v1. OAuth standards mode is a future adapter and must not be partially advertised.

### 6. API Contract Conventions

ADR 0010 settles the initial API conventions before handlers are written.

Required standards:

- Error response envelope
- Pagination shape
- Filtering and sorting conventions
- Idempotency behavior for create and delete operations
- Audit field format and timestamps
- Stable resource ID format

Accepted direction:

- Publish these conventions before endpoint implementation starts.
- Keep resource IDs opaque.
- Use a single JSON error schema across REST and MCP-adjacent HTTP responses where possible.

Accepted API conventions:

- Use JSON request and response bodies throughout.
- Use `application/problem+json` conforming to RFC 9457 for REST errors.
- Use opaque UUIDv7 resource IDs and do not expose semantics in them.
- Use RFC 3339 UTC timestamps.
- Use `limit` and integrity-protected, query-bound `cursor` pagination where growth is expected; default 50, maximum 200, cursor lifetime 24 hours.
- Use deterministic validation errors with machine-readable codes.
- Keep admin list endpoints simple and predictable before adding advanced filtering grammar.
- Use endpoint-specific filter allowlists and `sort=<field>` or `sort=-<field>`; reject unknown fields rather than passing a free-form query language to storage.
- Limit ordinary JSON bodies to 1 MiB, headers to 16 KiB, read headers to five seconds, ordinary requests to 30 seconds, idle connections to 60 seconds, and graceful drain to 30 seconds unless a bounded streaming contract overrides them.
- Retain idempotency results for 24 hours and require strong ETags with `If-Match` for mutable resources.

Error envelope:

```json
{
  "type": "https://appliance.local/problems/validation-error",
  "title": "Validation failed",
  "status": 400,
  "code": "validation_error",
  "detail": "username is required",
  "instance": "/api/v1/users",
  "requestId": "01K1ABCDEF1234567890XYZABC",
  "errors": [
    {
      "field": "username",
      "code": "required",
      "message": "username is required"
    }
  ]
}
```

ID format:

- Use UUIDv7 for primary resource identifiers.
- Use the same ID family across users, roles, tokens, builds, artifacts, and audit events.
- Keep token display prefixes separate from resource IDs.
- Never encode role names, timestamps, or resource type meaning into public IDs beyond the UUID format itself.

### 7. Authorization Granularity

ADR 0010 selects appliance-wide roles plus ownership checks for tokens, builds, and artifacts produced by builds. Registry pull/push/delete actions remain explicit permissions and each requested action is intersected with matching user/role repository-prefix grants. Developers receive personal/build prefixes, automation identities require explicit prefixes, and a full project model remains deferred. Every authorization call carries normalized resource context.

### 8. Audit And Compliance Surface

Audit logging is in the plan, but the event model should be explicit before code lands.

The event model must define:

- Which events are always auditable
- Which request attributes are stored
- Which secrets must be redacted
- Retention and export behavior

Minimum auditable events:

- login success and failure
- logout
- token creation and revocation
- password reset
- user creation, disable, role change
- build submission and cancellation
- registry access-token issuance, expiry, signing-key rotation, and source-credential revocation
- privileged MCP calls

ADR 0010 sets default audit retention to 365 days, configurable from 90 to 3650 days. Security mutations and their audit records commit in one transaction; audit storage failure therefore fails the mutation closed. Daily hash-chain checkpoints are included in off-appliance export and backup.

### 9. Upgrade And Schema Migration Story

ADR 0004 and ADR 0006 select explicit embedded, versioned SQL migrations. Upgrade preflight creates and verifies a coordinated backup, acquires an exclusive migration lock, and records migration state. The server migrates a known older schema once, refuses a newer or dirty/partially applied schema, and never silently downgrades. Rollback restores the pre-upgrade recovery set.

## Execution-Level Workstreams

Implementation will go more smoothly if we treat the work as parallel tracks with clear handoffs.

### Workstream A: Core Service Skeleton

- Go module and dependency policy
- App wiring and lifecycle
- Config model with environment and file support
- Structured logging
- Health and readiness endpoints
- Request IDs, correlation IDs, and middleware

### Workstream B: Storage And Migrations

- Repository interfaces
- SQL schema
- Migration runner
- Bootstrap seed data for built-in roles and admin
- Transaction boundaries and optimistic concurrency rules

### Workstream C: Identity

- Local password auth provider
- JWT session issuance and validation
- API token generation, hashing, lookup, and revoke
- Session and token revocation behavior
- Auth middleware for session and token modes

### Workstream D: Authorization

- Built-in permissions
- Built-in roles
- Policy evaluation helpers
- Resource ownership hooks
- Authorization middleware and denial logging

### Workstream E: Admin APIs

- Auth endpoints
- User lifecycle endpoints
- Role endpoints
- Token endpoints
- Consistent API error and response envelopes

### Workstream F: Workload APIs

- Build request model
- Build state machine
- Argo workflow-engine adapter, template catalog, reconciliation, cancellation, and TTL handling
- Rootless Buildah workflow task implementation
- Skopeo image inspection/copy/promotion adapter
- ORAS generic artifact adapter
- Syft SBOM and Grype vulnerability adapters with verified tool/database evidence
- Artifact metadata model
- OCI token issuer, scope translator, ORAS workflow boundary, and zot adapter

### Workstream G: MCP Surface

- MCP request envelope parsing
- Auth integration
- RBAC integration
- Capability advertisement
- Placeholder tool responses

### Workstream H: Operations And Packaging

- K3s manifests or charts
- Argo CRDs, namespace-scoped controller, workflow RBAC, and build namespace
- Traefik ingress config
- Secret wiring
- Backup and restore hooks
- Upgrade path
- Installer and release packaging

## Execution Task Ledger

Execute tasks in ID order unless their dependency column allows parallel work. A task is complete only when its evidence is checked into this repo or the release repo named by the packaging contract.

| ID | Executable outcome | Depends on | Completion evidence |
| --- | --- | --- | --- |
| E0-01 | Validate and pin Go, K3s, Traefik, Argo Workflows, zot, Buildah, Podman, Skopeo, ORAS, Helm, Syft, Grype, release-only Cosign, SQLite driver, and MCP revisions/digests starting from [compatibility candidates](compatibility-candidates.md) | None | Machine-readable compatibility manifest and license/provenance inventory |
| E0-02 | Record data classification, trust boundaries, abuse cases, and accepted v1 security claims | E0-01 | Reviewed threat model covering auth, builds, registry, K3s, backup, and supply chain |
| E0-03 | Prove zot external-token flow and selected full/minimal extension set | E0-01 | ADR 0008 test report and rating at or above its acceptance threshold |
| E0-04 | Prove namespace-scoped Argo plus rootless non-privileged Buildah in the supported K3s/host configuration | E0-01, E0-02 | ADR 0003/0011 report including CRD/RBAC scope, workflow lifecycle, storage driver, isolation, cleanup, cancellation, restart, and OCI output |
| E0-05 | Prove SQLite migration, disk-full, online backup, corruption detection, and clean restore | E0-01 | ADR 0004 test report and fixture scripts |
| E0-06 | Validate ADR 0010 canonical origin, listeners, TLS/network/configuration, auth/RBAC, audit, telemetry, supply-chain, support, and zot defaults | E0-02, E0-03, E0-04, E0-05 | Versioned validation evidence with no failed P0 gate |
| E1-01 | Create Go module, command entrypoint, package boundaries, dependency policy, Makefile, and CI/local verification lane | E0-01 | `make verify` passes on the supported local development host |
| E1-02 | Implement typed configuration, redacted logging, public/internal servers, middleware, probes, version, and shutdown | E1-01, E0-06 | Unit, race, fuzz-smoke, timeout, proxy, host-header, and shutdown tests |
| E1-03 | Implement storage interfaces, SQLite adapter, migration runner, transaction helper, and backup hook | E1-01, E0-05 | Repository contract, migration, concurrency, and restore tests |
| E2-01 | Implement bootstrap, local password provider, sessions/refresh, API tokens, account state, and break-glass | E1-02, E1-03 | End-to-end identity lifecycle and credential-redaction tests |
| E2-02 | Implement permission catalog, built-in/custom roles, authorization service, last-admin invariant, and audit | E2-01, E0-06 | Route/resource authorization matrix and durable audit tests |
| E2-03 | Publish and implement OpenAPI auth/user/role/token contracts and generated or conformance-tested client fixtures | E2-01, E2-02 | OpenAPI lint, compatibility, positive, and negative HTTP suites |
| E3-01 | Implement the pinned MCP Streamable HTTP shell with empty tools and shared auth/RBAC | E2-02 | MCP conformance/interoperability and REST-equivalence tests |
| E4-01 | Create Helm chart, K3s values, Traefik routes, Argo CRDs/controller, security policies, storage, secret wiring, and complete air-gap inputs | E1-02, E0-06 | Render/schema/policy tests plus egress-denied clean-host install and restart smoke tests for the one complete topology |
| E5-01 | Implement registry token issuer, scope intersection, signing/rotation, and `zotadapter` | E2-02, E0-03, E4-01 | Podman/Buildah/Skopeo/Helm/ORAS auth, deny, expiry, revoke, and rotation suite |
| E5-02 | Implement artifact catalog/referrers, Skopeo image operations, ORAS artifact operations, quotas, retention, and reconciliation | E5-01 | OCI conformance, media-type, referrer, extension-RBAC, GC/scrub/dedupe, and restore suite |
| E6-01 | Implement build API/state machine, Argo workflow adapter/templates, Buildah tasks, credential files, logs, cancellation, deadlines, TTL, and cleanup | E2-02, E0-04, E5-01 | Success/failure/security/resource/controller-restart/control-plane-restart matrix with immutable output attribution |
| E6-02 | Add Skopeo post-build verification, Podman runtime smoke test, Syft SBOM, Grype vulnerability evidence, provenance attachment, and artifact linkage | E6-01, E5-02 | Digest, policy, provenance, SBOM, scan-database, redaction, and cleanup evidence |
| E7-01 | Implement backup, clean-node restore, upgrade, restore-based rollback, diagnostics, and support bundle | E4-01, E5-02, E6-02 | Automated RPO/RTO, N-1 upgrade, failed-upgrade, and clean-node restore drills |
| E7-02 | Assemble the complete air-gap release-input closure and hand it to `appliance-release` | E7-01 | Signed manifests/checksums, required host-package inventory, all runtime images/data, SBOMs, provenance, notices, compatibility data, and egress-denied install evidence |

Parallelization guidance:

- E0-03, E0-04, and E0-05 can run in parallel after E0-01; all feed E0-06.
- E1-02 and E1-03 can run in parallel after E1-01.
- MCP work starts after shared authorization is stable; registry work starts after zot and K3s gates pass.
- Build implementation starts only after the Buildah isolation gate and registry publication path are proven.
- Release packaging automation may be scaffolded early, but release acceptance cannot bypass E7-01 recovery evidence.

## Additional Technical Concerns We Should Not Skip

These concerns must be assigned to the relevant phase rather than deferred to final hardening.

- Time handling: use UTC in storage and APIs, with RFC 3339 timestamps everywhere.
- Secret handling: define one place for signing keys, bootstrap credentials, and registry integration secrets.
- Observability: logs, metrics, health, and trace hooks should be part of the app skeleton.
- Background jobs: ADR 0010 selects one in-process maintenance manager with durable checkpoints for token/session cleanup, audit checkpoints/export scheduling, build reconciliation, and zot reconciliation; coordinated backup remains an external CronJob or installer operation.
- Concurrency control: define how duplicate token names, duplicate usernames, and repeated build submissions behave.
- API limits: define request size limits and timeout defaults.
- Input validation: centralize validation rules and username/password policy.
- Dependency boundaries: keep Kubernetes and registry-specific code out of the auth packages.
- Test strategy: unit tests for auth and RBAC, integration tests for HTTP flows, and cluster tests for build and registry flows.
- Local development path: define how to run the service without a full appliance install, ideally with a local SQLite mode and optional local k3d or k3s integration.

## Local-First Development Rule

The server must remain runnable as a normal local Go program throughout development.

Required properties:

- Developers must be able to build and run the server directly on a local machine without containers or K3s.
- Local tests must run by compiling and executing Go code directly on the host machine.
- Containerization and K3s packaging must be deployment layers, not prerequisites for normal development.
- The same application code paths should be used locally and in-container, with configuration selecting environment-specific behavior.
- Buildah/K3s security acceptance, and the control-plane's own container image build, run only on the Linux build server/CI — never on macOS, and never on a developer laptop regardless of platform. No container tooling (Podman, Buildah, etc.) is installed or used on macOS for this repo; see docs/dev-container.md.
- Unit and HTTP contract tests use in-process fakes at the Buildah, Skopeo, ORAS, zot, and Kubernetes interfaces. Separate integration lanes exercise the real pinned tools; fakes must not replace those release gates.

Recommended local workflow:

- `make build` builds the local server binary
- `make test` runs unit and integration tests that do not require K3s
- `make run` or equivalent starts the control plane locally using the SQLite-backed implementation
- optional `make dev-k3s` or similar can exercise the chart/manifests path later

Recommended top-level Make targets, inspired by `../forgeline/Makefile`:

- `make build`
- `make test`
- `make lint`
- `make coverage`
- `make verify`
- `make run`
- `make stop`
- `make dev-k3s`
- `make clean`

Recommended target intent:

- `build`: compile the main local binary
- `test`: local unit and integration tests
- `lint`: vet, staticcheck, and editor-grade checks where available
- `coverage`: repo or module coverage enforcement
- `verify`: local pre-push flow
- `run`: start the control plane locally with local config defaults
- `stop`: stop any locally started server
- `dev-k3s`: optional deployment path for chart/manifests validation
- `clean`: local artifact cleanup

Recommended config model:

- local mode defaults to SQLite and local filesystem paths
- future production mode can switch to Postgres configuration
- container mode uses the same binary with container-oriented paths and env vars
- K3s mode layers Kubernetes deployment concerns on top of the same binary and config surface

This is important both for local Mac development and for direct-machine validation on a build server before the full appliance packaging path is involved.

## Delivery Phases

Each phase is a releasable vertical slice with its security, operations, documentation, and tests included. A phase is complete only when its exit gate passes locally and, where applicable, in the supported K3s lane.

### Phase 0: Decisions, Threat Model, And Spikes

Phase 0 in this repo's normal development environment is documentation and decision-capture only. A plain developer machine has no K3s cluster, no zot instance, no rootless Buildah/appliance filesystem, and no appliance storage class to test against, so none of the P0 validation gates below can be executed or claimed as passed from local development. Coding work starts at Phase 1 (local Go service foundation) in parallel with Phase 0 documentation; the actual spikes run later against a real target host or cluster, coordinated with `appliance-release`, before the phase that depends on their gate.

Deliverables achievable now, from documentation and decisions alone:

- Maintain the accepted P0 ADRs and decision register; add data classification, trust-boundary diagram, and initial threat model.
- Record candidate Go, K3s/Kubernetes, Traefik, zot full-image/extension set, Buildah, Podman, Skopeo, ORAS, Helm, Syft, Grype, release-only Cosign, SQLite driver, and MCP protocol versions in the compatibility manifest (see [compatibility candidates](compatibility-candidates.md)); this captures intended versions, not verified release pins.
- Define token/session semantics, canonical external URL, and the initial permission matrix as design artifacts (already captured above in this plan and in ADR 0010).

Deliverables that require a real host or cluster and cannot be produced here:

- zot token-auth, ORAS, extension-isolation, storage, air-gap, and OCI conformance spike with Podman/Skopeo/Buildah/Helm/ORAS black-box tests (ADR 0008).
- Build-engine and isolation spike proving the selected rootless Buildah path does not require privileged mode, a host runtime socket, or an unaccepted security-profile exception (ADR 0003).
- SQLite volume, backup, disk-full, and restore spike on the intended appliance filesystem/storage class (ADR 0004).
- Validation of the accepted RPO/RTO, TLS, and storage ownership assumptions against real infrastructure (ADR 0006, ADR 0010).

Sequence to run later on a real target host/cluster, before the phase that gates on it:

1. Provision the supported Ubuntu 24.04 LTS/amd64/ext4 host (or equivalent CI runner) with the pinned K3s release installed air-gapped, per ADR 0001. This host is the only place E0-03 through E0-06 evidence may be produced.
2. E0-03 (before Phase 5): deploy the pinned zot image; run login/pull/push, ORAS/referrers, denied/malformed-scope, token-expiry, user-disable/token-revocation, extension-authorization, storage restart/disk-exhaustion/scrub/dedupe/GC, air-gap, and OCI Distribution conformance tests from real Podman/Skopeo/Buildah/Helm/ORAS clients. Record the ADR 0008 rating and test report.
3. E0-04 (before Phase 6): deploy the namespace-scoped Argo Workflow Controller and a rootless Buildah task pod under the pinned Pod Security Admission policy; prove no privileged mode, no host runtime socket, and no unaccepted security-profile exception is required. Record the ADR 0003/0011 report.
4. E0-05 (before Phase 1's storage work is trusted for production, and again before Phase 7): run the SQLite volume/backup/disk-full/restore spike on the real appliance filesystem or storage class. Record the ADR 0004 test report and fixture scripts.
5. E0-06 (before Phase 4): with E0-03 through E0-05 evidence in hand, validate the full ADR 0010 default set (canonical origin, listeners, TLS/network/configuration, auth/RBAC, audit, telemetry, supply-chain, support, zot defaults) on the same host. Record versioned validation evidence; a failed gate reopens the relevant ADR rather than being silently waived.

Exit gate (only claimable once the real-host sequence above has run):

- All P0 ADR validation gates pass on the pinned dependency versions.
- zot login/pull/push, ORAS/referrers, deny/revoke, extension filtering, storage/scrub/dedupe/GC, restore/upgrade, air-gap, and OCI conformance tests pass against the pinned image.
- The build threat model supports an honest v1 product claim.
- SQLite backup restores onto a clean test instance and passes integrity/application smoke checks.

### Phase 1: Service Foundation And Local Lane

Deliverables:

- Go module, server entrypoint, dependency policy, config schema/validation, structured redacted logging, version metadata, and graceful shutdown.
- Separate public and internal listeners; request IDs, panic recovery, trusted-proxy handling, host validation, limits, and timeout middleware.
- Liveness, readiness, and startup endpoints with tested semantics.
- Storage interfaces, SQLite adapter, explicit migrations, transaction helper, and deterministic test fixtures.
- Local Make targets for build, test, coverage, lint, verify, run, stop, clean, migration checks, and API generation/validation.

Exit gate:

- `make verify` succeeds without containers or K3s.
- Server starts locally from an empty directory, migrates once, restarts without mutation, drains cleanly, and fails safely for invalid config/schema.
- Race, fuzz-smoke, migration, disk-full/error-injection, and backup/restore tests cover the foundation.

### Phase 2: Identity, Sessions, Tokens, RBAC, And Audit

Deliverables:

- One-time bootstrap and node-local break-glass commands.
- Local password provider, session/refresh model, API token lifecycle, account state, built-in roles/permissions, and a single authorization service.
- Auth, user, role, and token APIs generated from or checked against OpenAPI.
- Durable audit schema and events transactionally coupled to security mutations where practical.
- Login and token rate limits, last-admin invariant, key rotation path, and credential redaction tests.

Exit gate:

- End-to-end bootstrap, login, refresh/reuse rejection, logout, user disable, password reset, token create/revoke/expire, role change, and break-glass tests pass.
- A table-driven authorization matrix proves allow and deny behavior for every route and built-in role.
- No raw credential appears in logs, errors, metrics, database dumps, or support-bundle fixtures.

### Phase 3: MCP Protocol Shell

Deliverables:

- Pinned Streamable HTTP/JSON-RPC implementation at `/mcp` with protocol negotiation, empty capability/tool behavior, limits, and appliance RBAC mapping.
- Appliance API-token Bearer compatibility mode documented and implemented without publishing OAuth protected-resource metadata.
- A clean authorization-provider interface for later MCP OAuth standards mode, with no partial standards-mode behavior in v1.
- Interoperability tests with at least two intended MCP clients or SDK conformance fixtures.

Exit gate:

- Valid initialize and empty `tools/list` flows pass; malformed, unsupported-version, unauthorized, forbidden, oversized, cross-origin, and concurrency-limit cases fail correctly.
- REST and MCP authorization tests produce equivalent decisions for the same principal and permission.

### Phase 4: K3s Deployment Baseline

Deliverables:

- Helm chart as the primary deployment package plus minimal raw manifests/values for debugging and CI.
- Traefik routes, Argo CRDs and namespace-scoped Workflow Controller, TLS secret modes, persistent volume, probes, security contexts, Pod Security Admission, RBAC, NetworkPolicies, quotas, and pinned images for the single complete topology.
- K3s-supported version matrix, host prerequisites, internal DNS/NTP, offline image-preload contract, and capacity floor.
- Automated install, uninstall-with-data-preservation, restart, node-reboot, certificate, and smoke tests.

Exit gate:

- Chart lint/schema/render tests and policy checks pass.
- A clean supported host can install the appliance, survive pod and node restart, preserve state, reject direct internal-service access, and produce a redacted support bundle.
- Local binary and K3s lanes pass the same API conformance suite.

### Phase 5: OCI Registry Vertical Slice

Deliverables:

- zot adapter, pinned full/minimal image and extension configuration, storage, health, and reconciliation.
- Standard OCI registry token endpoint, scope parser/intersection, registry-specific signing keys, rotation, and audit events.
- Explicit ownership of OCI metadata/referrers, repository namespaces, media types, quotas, retention/delete policy, and event/index reconciliation.
- ORAS-backed generic artifact workflows, extension ingress policy, scrub/dedupe/garbage collection, storage backup, metadata rebuild, and clean-node restore procedure.

Exit gate:

- Podman/Skopeo/Buildah/Helm/ORAS login and data flows, denied and malformed scope, expiry, disabled user, revoked API token, key rotation, registry restart, quota/full-storage behavior, scrub/dedupe/GC, extension authorization, and clean-node restore tests pass.
- Applicable OCI Distribution conformance tests pass and registry internals remain inaccessible through product ingress.

### Phase 6: Build Vertical Slice

Deliverables:

- Build API/state machine, idempotency, Argo template catalog/adapter, status reconciliation, logs, explicit cancellation, deadlines, workflow/pod TTL, cleanup, and retry rules.
- Isolation controls selected in Phase 0, per-build identities/secrets, network policy, quotas, concurrency, approved builder images, artifact publication, and provenance metadata.
- Recovery reconciliation after control-plane, Argo Workflow Controller, or K3s restart.

Exit gate:

- Successful, failed, timed-out, cancelled, duplicate, unauthorized, malicious-input, resource-exhaustion, egress-denied, secret-redaction, orphan-cleanup, and restart-recovery tests pass.
- Produced artifacts are attributable to principal, source revision, build definition, and immutable builder digest.

### Phase 7: Appliance Release Candidate

Deliverables:

- Installer wrapper contract for `appliance-release`, offline bundle inputs, signed checksums/manifests, SBOMs, provenance, vulnerability/license reports, and compatibility manifest.
- Automated preflight, install, upgrade, rollback/restore, backup scheduling, restore command, diagnostics, and release notes.
- Operations guide, security guide, API/MCP docs, backup/restore runbook, upgrade runbook, and break-glass runbook.

Exit gate:

- Fresh air-gapped install tests pass with public egress denied on every supported platform.
- Upgrade from the previous supported version and clean-node restore drills meet RPO/RTO targets.
- Security scans meet the documented severity policy, release artifacts verify offline, and all acceptance criteria below have evidence.

## Decision Log Before Coding

The authoritative status is in the [decision register](decision-register.md). Track each item as `proposed`, `accepted`, or `superseded`, with owner, date, rationale, consequences, and verification evidence.

Already accepted:

- One Go server for REST and MCP, with shared authn/authz.
- Dedicated, product-managed single-node K3s with Traefik; Helm under an installer wrapper for release packaging ([ADR 0001](adr/0001-dedicated-k3s-appliance.md)).
- zot with control-plane-issued OCI access tokens and generic OCI/ORAS artifact support ([ADR 0008](adr/0008-zot-oci-artifact-registry.md)); the Distribution and Nexus decisions in ADRs 0007 and 0002 are superseded.
- Namespace-scoped Argo Workflows behind the control plane in the complete v1 appliance ([ADR 0011](adr/0011-argo-workflows-engine.md)).
- Trusted-only rootless Buildah workflow tasks for the first build implementation ([ADR 0003](adr/0003-trusted-build-boundary.md)).
- Buildah, Podman, Skopeo, ORAS, zot, and Helm responsibilities are fixed by the explicit OCI toolchain contract ([ADR 0009](adr/0009-oci-toolchain.md)).
- SQLite behind storage interfaces for the control plane; one replica while SQLite is active ([ADR 0004](adr/0004-control-plane-sqlite.md)).
- Purpose-separated keys and explicit production TLS modes ([ADR 0005](adr/0005-secrets-keys-and-tls.md)).
- Daily off-appliance recovery sets, embedded-etcd K3s snapshots, N-1 upgrades, and restore-based rollback ([ADR 0006](adr/0006-backup-upgrade-and-recovery.md)).
- Concrete v1 security and operations defaults ([ADR 0010](adr/0010-v1-security-and-operations-defaults.md)).
- One complete signed air-gap package with no install-time or runtime public-network dependency ([ADR 0012](adr/0012-offline-first-appliance.md)).
- Local binary build/test/run remains supported.

Must be pinned or completed before Phase 1:

- Exact dependency versions and image digests in the compatibility manifest.
- Validation of the initial Ubuntu/amd64/ext4 sizing and host preflight contract.
- Validation of ADR 0010 public/internal listeners, canonical origin, TLS, internal DNS/NTP, denied public egress, IPv4-only stance, configuration precedence, data classification, and 365-day audit default.
- SQLite driver compatibility, online-backup implementation, and failure-injection tests required by ADR 0004.
- Validation of ADR 0010 session, refresh, API-token, password, abuse-rate, last-admin, built-in role, permission, and ownership semantics.

Must be pinned or validated before the affected vertical slice:

- MCP protocol revision and API-token compatibility-mode client interoperability evidence.
- zot version/image digest, accepted full-image extension profile, rating evidence, and the complete ADR 0008 auth/OCI/ORAS/storage/recovery conformance gate.
- Buildah/Podman/Skopeo versions and the complete ADR 0003 K3s security-context, storage-driver, and OCI-output validation gate.
- ADR 0009 shared auth-file contract, registry/certificate trust policy, base-image/source allowlists, Syft/Grype SBOM/scanning, Sigstore-compatible signing, and air-gap import behavior.
- Clean-node recovery and N-1 upgrade evidence required by ADR 0006.
- Validation of ADR 0010 outbound-telemetry-off, vulnerability gate, release-signing trust model, and N/N-1 support policy.

## K3s Appliance Development And Packaging Strategy

There are two separate concerns here, and it helps to treat them separately:

- product code that defines appliance behavior
- release packaging that turns product code into a user-installable appliance

### What Should Live In This Repo

I would keep the following here, because they are part of the product contract and need to evolve with the server code:

- control plane server code
- API and MCP contracts
- database schema and migrations
- built-in roles and permission seed data
- Kubernetes deployment manifests or Helm chart for the control plane and directly-coupled services
- Argo Workflows CRD/controller configuration, workflow templates, and compatibility tests for the complete topology
- local development deployment assets
- smoke tests that prove the server works on K3s
- local Make targets and scripts for direct host execution without containers

### What Lives In `appliance-release`

The public release repo owns artifacts that are release-engineering oriented rather than product-code oriented:

- appliance installer and lifecycle CLI
- complete air-gap bundle assembly
- signed release manifests
- public-facing packaging metadata
- release notes and upgrade channel metadata
- image pinning, including Argo controller/executor images, and bill-of-materials snapshots
- offline update and air-gap distribution packaging

### Accepted Split

The two-repository model is accepted. This repo is the release-input producer; `appliance-release` is the installer and final-bundle producer. The release repo consumes immutable signed outputs and must not clone private source, rebuild the server, fork the Helm chart, or redefine product security behavior.

The complete ownership matrix and handoff contract are in [Repository Boundary](repository-boundary.md). The release repo carries its own executable plan so both workstreams can proceed independently against the same versioned contract.

## Accepted Initial Install Path

V1 uses one complete air-gap bundle containing a lifecycle CLI, pinned K3s, all required OCI images and data, Argo CRDs, and the canonical Helm chart.

- `appliance-release` owns the user-facing lifecycle CLI and complete bundle.
- The lifecycle CLI performs host preflight, installs bundled K3s, preloads images, applies CRDs, installs the chart, generates secrets, bootstraps the server, and verifies health.
- Helm is the Kubernetes packaging primitive underneath the CLI.
- Raw manifests remain development and debugging assets in this repo, not another product package.
- There is no connected installer, release-bundle profile selection, remote fallback, or installation onto arbitrary existing clusters in v1.
- The same bundle and code path are used whether or not the target host happens to have internet connectivity.

### Argo Packaging Contract

Argo introduces cluster-scoped CRDs whose lifecycle cannot be delegated casually to normal Helm templating. Package it as follows:

- This repo owns the tested Argo integration: typed workflow templates, namespace-scoped controller configuration, RBAC, NetworkPolicies, chart values/schema, and conformance tests.
- Publish the exact Argo CRDs as a separate versioned release input. The installer applies or upgrades that bundle before the appliance Helm release, verifies the served/storage versions, and refuses an unsupported downgrade.
- The appliance chart always creates the workflow/build namespaces, controller, service accounts, policies, quotas, and template configuration in v1. It never deploys Argo Server/UI or a workflow-archive database.
- Do not add an Argo-disable value or artifact-only chart variant in v1. Modularity remains an internal code boundary, not a packaging choice.
- The air-gap bundle includes pinned Argo controller/executor images, CRDs, licenses/notices, SBOM/provenance, and the compatibility evidence tied to the K3s release.
- Upgrade order is preflight and backup, quiesce workflow submission, reconcile/stop in-flight workflows, upgrade CRDs, upgrade controller/chart, verify reconciliation, then re-enable submissions. Restore uses the release version recorded in the recovery set.

## Recommendation On Database Path

Given your requirement that local development must stay simple and direct, my recommendation is:

```text
Use one storage interface.
Implement SQLite as the first adapter.
Run local development on that same SQLite adapter.
Add Postgres later as the next adapter.
```

Why this fits the current first-cut preference:

- it preserves a single persistence behavior for now
- it keeps local development simple
- it still forces us to define clean storage interfaces up front
- it leaves the local-versus-production split as a later decision instead of a day-one complexity

What we must be disciplined about:

- auth, RBAC, token, and audit logic should live above the storage layer
- repository contracts should be explicit enough that a later Postgres adapter can slot in cleanly
- SQL and migrations should be kept organized even in the SQLite phase

So my preferred sequence is:

1. implement the storage interfaces now
2. implement the SQLite-backed adapter
3. use it for local development and the first cut of the server
4. add the Postgres-backed adapter later
5. revisit local-versus-production persistence split only if it becomes necessary

## OCI Registry Integration Direction

ADR 0008 makes zot the OCI image and generic artifact data plane behind the appliance control plane.

Accepted direction:

- The appliance control plane owns user lifecycle, API-token validation, RBAC, repository scope translation, registry-token signing, audit, and artifact lifecycle intent.
- zot owns upload/download protocol handling and OCI manifest, tag, digest, referrer, and blob storage.
- Podman, Skopeo, Buildah, Helm, and ORAS receive a standard `WWW-Authenticate` challenge pointing to `/api/v1/registry/token`.
- Registry access tokens have a five-minute maximum lifetime and carry only the repository actions currently allowed by appliance RBAC.
- Backup and restore cover control-plane state/keys and the zot storage PVC plus required extension state; no separate registry database exists in v1.

Recommended adapter boundary (all paths below are relative to `services/controlplane/`):

- `internal/artifacts/` owns appliance artifact-domain use cases and metadata.
- `internal/registryauth/` owns OCI challenge parameters, scope parsing/intersection, registry JWT claims, signing, and key rotation.
- `internal/zotadapter/` owns zot API calls, health, events, extension/index status, storage reconciliation, and error translation.
- `internal/authz/` remains transport- and registry-product-neutral.
- `internal/users/` and `internal/tokens/` do not import zot concerns.

The zot adapter must not own users, API tokens, RBAC policy, audit policy, REST/MCP handlers, or raw signing keys. It receives already-authorized domain operations or verifies only the data-plane state needed for reconciliation.

The adapter interface preserves CNCF Distribution as a fallback and allows future non-OCI package modules without changing appliance identity or authorization.

## Suggested Bootstrap And Break-Glass Commands

Recommended bootstrap command shape:

```text
appliance-server bootstrap init
```

Purpose:

- initialize schema
- create built-in roles and permissions
- create the first admin user
- mark bootstrap as completed

Required inputs:

- `--admin-username`
- `--admin-password-file`
- `--hostname`

Bootstrap must require an uninitialized database and an installer-created one-time authorization artifact. Do not provide a production `--force` path that can overwrite an initialized security database.

Recommended break-glass command shape:

```text
appliance-server recovery reset-password --username <username> --password-file <path>
```

Guardrails:

- allow only local-node execution
- require an exclusive application/database lock
- require explicit terminal invocation, not remote API access
- emit prominent audit events
- require an interactive confirmation unless an explicit non-interactive recovery procedure supplies a protected confirmation file
- revoke all sessions and API tokens when resetting a password
- refuse remote invocation and refuse to operate on a database that is not owned by the local appliance instance

Operational rule:

- bootstrap commands are for day-0 initialization
- break-glass commands are for node operators with machine access
- neither should become a normal administrative workflow once the control plane is healthy

## Suggested Packaging Contract Between Repos

The release repo consumes versioned artifacts from this repo rather than re-owning server behavior.

Suggested outputs from this repo:

- versioned control plane image
- versioned Helm chart or manifest bundle
- versioned Argo CRD bundle, controller/executor image identities, and workflow-template catalog
- migration bundle
- default configuration schema
- compatibility matrix for registry and Kubernetes versions
- smoke-test or conformance test suite for release validation
- local binary build outputs and local run/test workflow docs

## Recommended Near-Term Directory Strategy

Under the accepted split, this repo should grow these top-level areas, per the multi-service layout in [Proposed Repo Structure](#proposed-repo-structure):

```text
services/
  controlplane/
    cmd/
    internal/
sdk/
  golang/
    applianceclient/
deploy/
  dev/
  k3s/
  traefik/
  charts/
    appliance-control-plane/
scripts/
  dev/
  test/
  package/
docs/
```

Meaning:

- `deploy/dev` for fast local and CI environments
- `deploy/k3s` for appliance-oriented manifests
- `deploy/charts/appliance-control-plane` for the appliance chart, including optional Argo controller and workflow CRD/profile wiring
- `scripts/package` only for producing and validating this repo's signed release-input closure; host installers and final air-gap bundle assembly belong in `appliance-release`

## Acceptance Criteria For V1

V1 is successful only when functional, security, recovery, and packaging evidence all exist.

Functional acceptance:

1. One-time bootstrap creates the initial administrator and cannot be replayed remotely.
2. Login, refresh rotation, logout, password reset, disable/enable, token creation, expiration, and revocation follow the documented semantics.
3. The last effective administrator cannot be removed or disabled accidentally.
4. REST and MCP use the same identity and authorization decisions; MCP conforms to its pinned protocol and documented auth mode with an empty tool set.
5. Podman, Skopeo, Buildah, Helm, and ORAS login and authorized/denied image/artifact operations, malformed scope rejection, expiry, disable, and revocation work through the standard OCI token flow.
6. Builds run through appliance-generated Argo Workflows and support idempotent create, status, logs, cancel, timeout, controller/control-plane restart reconciliation, cleanup, and artifact attribution without exposing Argo directly.

Security acceptance:

7. Route-by-role tests cover every protected REST, MCP, registry, and build action with positive and negative cases.
8. Kubernetes workloads pass manifest/policy checks for least privilege, restricted security contexts, network isolation, secret handling, resource limits, and immutable image pins.
9. Credential leakage, account enumeration, malformed input, oversized requests, timeout, concurrency, SSRF/egress, and audit-redaction tests pass.
10. Images and release artifacts have verifiable checksums/signatures, SBOMs, provenance, and vulnerability/license results that meet policy.

Reliability and operations acceptance:

11. The appliance survives process, pod, Argo Workflow Controller, zot, and node restart without state loss or duplicate artifact publication and reports dependency degradation accurately.
12. Automated backup and clean-node restore cover control-plane state, keys, K3s recovery material, Argo CRDs/configuration/templates and in-flight reconciliation policy, zot storage and required extension state, certificates, and configuration within stated RPO/RTO.
13. Disk/inode exhaustion, corrupt/incompatible schema or artifact content, expired certificate, unavailable Kubernetes API, unavailable zot, and interrupted migration/upgrade fail safely and produce actionable diagnostics.
14. Upgrade from every supported source version and the declared rollback or restore path pass automated tests.
15. Capacity metrics, alerts, bounded logs, and a secret-redacted support bundle are documented and tested.

Delivery acceptance:

16. Direct local Go build/test/run and K3s deployment pass the same API conformance suite.
17. Fresh installs from the complete air-gap bundle pass with public egress denied on the supported OS/architecture matrix.
18. OpenAPI, MCP behavior, configuration schema, compatibility matrix, operations, security, backup/restore, upgrade, and break-glass documentation match the released artifacts.
19. The identity-provider boundary can add OIDC/SAML/LDAP without changing domain users, API tokens, RBAC, audit, or feature modules.

## Recommended Early Implementation Order

Follow the gated phases above in order:

1. Resolve P0 decisions and prove zot auth/OCI/ORAS/extension/storage/recovery conformance, build isolation, and SQLite recovery.
2. Build the local service/storage foundation.
3. Deliver identity, RBAC, and audit as one secured vertical slice.
4. Deliver the MCP protocol shell and its chosen authorization mode.
5. Establish the hardened K3s/Traefik deployment baseline.
6. Add OCI registry and build features as separate tested vertical slices.
7. Produce the appliance release candidate with recovery, upgrade, air-gap, and supply-chain evidence.

## Standards And Primary References

Pin exact revisions in ADRs and the release compatibility manifest. Initial baselines:

- MCP specification `2025-11-25`: [Streamable HTTP transport](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports) and [HTTP authorization](https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization).
- [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) for HTTP problem details, [RFC 9110](https://www.rfc-editor.org/rfc/rfc9110) HTTP semantics, [RFC 3339](https://www.rfc-editor.org/rfc/rfc3339) timestamps, and the [OpenAPI 3.1 specification](https://spec.openapis.org/oas/v3.1.1.html).
- [OAuth Protected Resource Metadata (RFC 9728)](https://www.rfc-editor.org/rfc/rfc9728), [Resource Indicators (RFC 8707)](https://www.rfc-editor.org/rfc/rfc8707), [Bearer Token Usage (RFC 6750)](https://www.rfc-editor.org/rfc/rfc6750), and current OAuth security best practices when MCP standards auth is enabled.
- [OCI Distribution Specification](https://github.com/opencontainers/distribution-spec) for registry behavior.
- zot [configuration](https://zotregistry.dev/v2.1.18/admin-guide/admin-configuration/), [authentication](https://zotregistry.dev/v2.1.15/articles/authn-authz/), [storage](https://zotregistry.dev/articles/storage/), and [ORAS workflows](https://zotregistry.dev/v2.1.15/user-guides/user-guide-datapath/) for the pinned release.
- Buildah [project](https://github.com/containers/buildah) and [build/isolation contract](https://github.com/containers/buildah/blob/main/docs/buildah-build.1.md) for image construction.
- Skopeo [project and image-operation contract](https://github.com/containers/skopeo) for inspect, copy, verification, and air-gap synchronization.
- [Podman documentation](https://docs.podman.io/) for supported runtime smoke tests and registry-client behavior.
- [ORAS](https://oras.land/) for generic OCI artifact client behavior and media-type/referrer conventions.
- [Syft](https://github.com/anchore/syft) for CycloneDX/SPDX SBOM generation and [Grype](https://github.com/anchore/grype) for vulnerability scanning with a pinned offline database.
- [Sigstore Cosign](https://github.com/sigstore/cosign) for release-only manifest, provenance, and non-image blob signatures.
- [Kubernetes Security Checklist](https://kubernetes.io/docs/concepts/security/security-checklist/), [Pod Security Standards](https://kubernetes.io/docs/concepts/security/pod-security-standards/), RBAC good practices, NetworkPolicy, probes, Jobs, and Secrets guidance for the pinned K3s/Kubernetes version.
- K3s [backup/restore](https://docs.k3s.io/datastore/backup-restore), [air-gap install](https://docs.k3s.io/installation/airgap), private registry, upgrade, and security guidance for the pinned release.
- SQLite [transactions](https://www.sqlite.org/lang_transaction.html), [WAL](https://www.sqlite.org/wal.html), [online backup](https://www.sqlite.org/backup.html), integrity-check, and filesystem guidance for the pinned SQLite library.

## Notes For Future Profiles

Do not productize appliance profiles yet, but keep the domain model ready for combinations such as:

- artifact server only
- build server only
- combined build + artifact server
- future application-control workloads

The cleanest way to leave room for this is to model capabilities as feature modules behind one control plane, rather than as separate identity or API stacks.

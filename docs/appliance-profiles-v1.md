# Appliance Profiles V1

## Purpose

This document defines the v1 appliance-profile model for the control plane.
It establishes the product-facing `appliance profile` concept, the
implementation-facing `appliance capability` concept, the initial v1
profiles and capabilities, and the rules the API server must enforce when a
profile is selected.

This is a control-plane behavior contract. It does not introduce multiple
bundle variants for v1. The appliance still ships as one complete signed
offline bundle.

## Terminology

- `appliance profile`: a product-level selection a user or installer makes
  for an appliance deployment
- `appliance capability`: an implementation-level functional switch resolved
  from the selected appliance profile
- `role` and `permission`: user-level authorization applied only after a
  capability is enabled

The control plane does not execute directly by profile. At startup it
resolves the selected `appliance profile` into an enabled
`appliance capability` set, and the server's routing and module wiring are
driven by that resolved capability set.

## V1 Goals

- Give the product named appliance profiles without hardcoding profile logic
  into handlers throughout the codebase.
- Keep profile-to-capability mapping changeable over time.
- Make appliance capabilities, not product profile names, drive runtime
  registration and initialization.
- Fail closed when a route or module requires a capability that is not
  enabled.
- Keep capability gating at the API-server layer so disabled features do not
  partially execute deeper in the stack.

## V1 Non-Goals

- Multiple released bundle variants
- Dynamic live profile switching without a process restart
- A user-facing custom capability editor in v1
- Treating RBAC as a substitute for appliance capability gating

## V1 Appliance Capabilities

The initial v1 appliance capabilities are:

| Capability | Purpose |
| --- | --- |
| `base` | Mandatory control-plane baseline: server startup, health/version surface, authentication/session shell, user/role/token administration, internal forward-auth checks, and the minimum API contract required for any appliance profile |
| `workflows` | Workflow substrate awareness and workflow-dependent module activation for v1 and future expansion |
| `build` | Build APIs and build service/module behavior |
| `artifact` | Artifact-facing APIs and module behavior; in the current v1 implementation this maps to OCI registry-token, grant, repository, and catalog flows backed by zot |

Notes:

- `base` is required for every appliance profile.
- `artifact` is the generic product term. The current implementation behind
  it is registry-oriented, but the capability name is intentionally not tied
  to a specific vendor or protocol brand.
- `/mcp` remains part of `base` in v1. If MCP later needs its own appliance
  capability, that can be split without changing the model.

## V1 Appliance Profiles

The initial v1 appliance profiles are:

| Appliance profile | Default | Resolved appliance capabilities |
| --- | --- | --- |
| `core` | Yes | `base`, `workflows` |
| `builder` | No | `base`, `workflows`, `build`, `artifact` |
| `storage` | No | `base`, `artifact` |

Notes:

- `core` is the default v1 product profile.
- The mapping from appliance profiles to appliance capabilities is not a
  permanent public truth table. It is the v1 mapping and may evolve in later
  versions.
- The implementation should keep the mapping centralized so adding a new
  appliance profile does not require scattering profile checks across the
  server.

## Capability Dependency Rules

Capability dependencies must be declared explicitly in one place and
resolved by the control plane at startup. They must not be assumed
implicitly by unrelated handlers or services.

The v1 dependency set is:

| Capability | Depends on |
| --- | --- |
| `base` | none |
| `workflows` | `base` |
| `build` | `base`, `workflows`, `artifact` |
| `artifact` | `base` |

Rules:

- The selected appliance profile resolves to a capability set.
- The server validates the resolved set against declared dependencies before
  any public listener starts.
- If the resolved set is invalid, startup fails with a configuration error.
- Future versions may change dependency declarations without changing the
  overall appliance-profile model.

## V1 Route And Module Mapping

The control plane should treat appliance capabilities as the authoritative
gate for route registration and feature-module activation.

### `base`

`base` owns the always-required control-plane identity and control surface,
including:

- internal health and version endpoints
- authentication endpoints under `/api/v1/auth/*`
- user-management endpoints under `/api/v1/users*`
- role and permission endpoints under `/api/v1/roles*` and
  `/api/v1/permissions`
- API-token endpoints under `/api/v1/tokens*` and
  `/api/v1/users/{userId}/tokens*`
- internal forward-auth endpoint at `/internal/auth/check`
- MCP endpoint at `/mcp`

`base` also owns the minimum service wiring behind those routes, including:

- configuration loading and validation
- key loading/generation
- SQLite migrations
- session, token, user, role, audit, and authorization services

### `workflows`

`workflows` governs workflow-engine awareness and workflow-oriented module
activation.

In v1, `workflows` does not yet introduce its own broad standalone public
REST surface. It primarily exists so the product can model workflow
availability explicitly and grow future workflow-facing APIs without
inventing a new profile system later.

### `build`

`build` owns build API registration and build service/module activation,
including:

- `/api/v1/builds`
- `/api/v1/builds/{id}`
- `/api/v1/builds/{id}/cancel`
- `/api/v1/builds/{id}/logs`

It also owns the build service and workflow-engine integration required to
accept, track, and manage build requests.

### `artifact`

`artifact` owns artifact-facing API registration and module activation.
In the current v1 implementation this includes:

- `/api/v1/registry/token`
- `/api/v1/registry/grants`
- `/api/v1/registry/grants/{id}`
- `/api/v1/registry/repositories`
- `/api/v1/registry/repositories/{rest...}`

It also owns the control-plane integrations that support those APIs,
including registry authorization policy and zot-backed repository/tag/
referrer access.

## Enforcement Rules

The API server must enforce the following rules:

1. Resolve the selected appliance profile into an appliance capability set
   before wiring handlers.
2. Register only the routes whose owning appliance capability is enabled.
3. Initialize only the modules whose owning appliance capability is enabled.
4. Return `404 Not Found` for requests to routes belonging to disabled
   appliance capabilities.
5. Never rely on RBAC alone to hide disabled features.
6. Fail startup if the selected appliance profile resolves to an invalid
   capability set or a required dependency is missing.

This keeps disabled features from partially executing and prevents
capability mismatches from leaking deeper into the business layer.

## Packaging And Release Implications

For v1:

- appliance profiles do not change the existence of the offline bundle
- appliance profiles do not create alternate installer artifacts
- appliance profiles do not authorize the release repo to fork charts or
  patch product behavior

Instead, the selected appliance profile is passed as product configuration
into the control plane, and the control plane resolves that into appliance
capabilities locally.

The complete v1 bundle still ships the full topology, including Argo and
zot. Appliance profiles change control-plane activation and exposure rules,
not the release-bundle shape.

## Implementation Direction

The intended v1 implementation shape is:

- one centralized appliance-profile definition table
- one centralized appliance-capability definition table, including
  dependencies
- one API/endpoint registry that records the owning appliance capability for
  each route
- one capability-aware module-wiring layer in the control-plane server

The server should avoid embedding profile logic directly in handlers. The
handler layer should receive only the routes and services that survived
capability resolution at startup.

## Future Evolution

This model is intentionally extensible.

Future versions may:

- add new appliance profiles
- change which appliance capabilities a profile resolves to
- add new appliance capabilities
- split `base` or move `/mcp` behind its own capability
- expose a limited capability-override model if product requirements demand
  it

Those changes should not require changing the basic contract that:

`appliance profile` -> `appliance capabilities` -> route and module
activation

# End-to-End Testing Plan

## Purpose

This document defines how this repo should validate the appliance control
plane end to end while preserving the core requirement that the Go server
must build, run, and be testable locally without K3s, containers, or
packaging.

The intent is to mirror the useful testing shape from `../forgeline`
without copying product-specific structure that does not apply here.

## Primary Decisions

- End-to-end tests live in a top-level `e2etests/` area, not inside
  `services/controlplane` or the SDK module.
- End-to-end tests exercise the real compiled server binary over its public
  HTTP surface using the Go SDK as the client.
- The local E2E lane is the required first lane and must run on a normal
  development machine with only Go installed.
- K3s and packaged-appliance E2E flows are separate lanes that reuse the
  same test client logic where possible, but they are not prerequisites for
  local development.
- The source repo owns local and source-tree-driven E2E flows. The
  `appliance-release` repo owns installed-appliance validation flows.

## Test Levels

### Level 0: Unit And In-Process Contract Tests

Existing tests remain in module-local packages:

- backend service tests
- HTTP handler tests
- MCP protocol/handler tests
- SDK client tests
- chart rendering/policy tests

These are fast feedback tests and should keep growing.

Alongside them, the backend should also keep a curl-based live HTTP
reference flow. That lane is not a substitute for the SDK-driven E2E suite;
it is a low-friction transport-level smoke and contract check that proves
the raw endpoint surface behaves correctly when the real server binary is
running locally.

### Level 1: Local Live-Server E2E

This is the first required end-to-end lane for this repo.

The flow is:

1. Build the real `appliance-server` binary locally.
2. Start it with isolated temp state on loopback addresses.
3. Run bootstrap init through the real CLI.
4. Run an external Go test client from `e2etests/` that talks only through
   `sdk/golang/applianceclient`.
5. Exercise the supported public APIs end to end.
6. Stop the server and retain logs/artifacts on failure.

This lane validates:

- config loading
- key generation/loading
- SQLite migrations
- bootstrap/recovery CLI surface
- real HTTP auth/session/token flows
- real RBAC enforcement
- request routing and middleware
- SDK-to-server compatibility
- local binary viability outside containers/K3s

### Level 2: Local Multi-Process Extended E2E

This is still local, but adds richer flows once the product surface grows.

Examples:

- build submission plus fake workflow progression
- registry grant creation plus registry token issuance
- MCP initialize/ping/tools list via live `/mcp`
- break-glass recovery commands against a running or stopped local server

This lane still runs without K3s.

### Level 3: Real Appliance Validation

This validates the packaged appliance on a Linux/K3s target.

This lane belongs primarily in `../appliance-release` because it must prove:

- bundle installation
- host prerequisite closure
- K3s bring-up
- Traefik routing
- control-plane pod behavior
- zot pod behavior
- Argo presence and packaging wiring
- offline install/upgrade/restore behavior

The source repo should supply reusable E2E client binaries or `go run`
entrypoints, but the release repo should own the install-and-validate
wrappers.

## Repository Layout

The target layout in this repo should be:

```text
e2etests/
  go.mod
  internal/
    serverproc/        # start/stop/wait helpers for the local server process
    testenv/           # temp dirs, ports, log paths, bootstrap helpers
    report/            # reusable failure/log summary helpers
  local/
    integration-test/  # local live-server test client
  appliance/
    integration-test/  # same client shape, target base URLs supplied externally
```

Notes:

- `e2etests/` should be its own Go module, like in `../forgeline`.
- The integration-test programs must not import backend `internal/...`
  packages.
- They may import only public packages such as the SDK and generic stdlib
  helpers.
- Shared helpers under `e2etests/internal` are test infrastructure only and
  must not become product runtime dependencies.

## Why Top-Level `e2etests/`

This is the cleanest boundary for the kind of validation we want:

- the tests are external clients, not white-box backend tests
- they validate the public SDK contract
- they should remain reusable against both local and packaged targets
- they should not be structurally tied to the backend module's internals

Putting them inside `services/controlplane` would blur the boundary and make it too
easy to reach into internal packages instead of using the SDK and public
APIs.

## Required SDK Expansion

The SDK is the supported client surface for REST-first E2E coverage. It now
covers the core local E2E paths:

- auth/session/refresh/logout
- users create/list/get/patch/disable/enable/unlock/password-reset/set-roles
- roles list/permissions/create/update/delete
- tokens create/list/revoke, including admin create/revoke for a user
- registry token, grants, repositories, tags, and referrers
- low-level builds create/list/get/cancel/logs
- developer workflow workspace profiles, workspaces/current workspace, build
  targets, build-target submission, jobs, job steps, job logs, and job cancel

Still to add when those product contracts are implemented:

- any future audit/system endpoints that are part of the supported public API

Rule:

- if a public API is part of the supported product contract, the E2E harness
  should reach it through the SDK, not ad hoc raw HTTP calls, unless the API
  is intentionally protocol-specific such as `/mcp`

## Local E2E Scope For Phase 1

Phase 1 local E2E should cover the highest-value control-plane flows first:

1. Server starts with empty state.
2. `bootstrap init` creates the first administrator.
3. Admin login succeeds.
4. Session introspection works.
5. Session refresh works.
6. Self API token creation/list/revoke works.
7. Admin creates a second user.
8. Admin lists users and reads that user back.
9. Admin creates a custom role and assigns it.
10. Permission enforcement is proven with a limited user.
11. Admin creates registry grants.
12. API-token-based registry token issuance works.
13. Build create/list/get/cancel works against the fake workflow engine.
14. MCP initialize/ping/tools list works through the live `/mcp` endpoint.
15. Recovery reset-password flow is proven node-locally.

This is enough to validate the basic front-facing control-plane contract
before K3s deployment enters the picture.

## Local E2E Execution Model

The local live-server harness should work like this:

- create a temporary working directory under `.run/e2e/local/<test-name>/`
- allocate dedicated public/internal loopback ports
- start the server with:
  - `APPLIANCE_PUBLIC_ADDR`
  - `APPLIANCE_INTERNAL_ADDR`
  - `APPLIANCE_CANONICAL_ORIGIN`
  - `APPLIANCE_DATA_DIR`
- wait for `/health/ready`
- invoke `appliance-server bootstrap init ...`
- execute the SDK-based scenario client
- on failure:
  - preserve server stdout/stderr log
  - preserve client log
  - preserve state directory
  - print exact artifact paths
- on success:
  - keep concise logs and optionally clean temp state

## Make Target Plan

The root repo should grow these targets:

- `make test-e2e`
  - runs the local live-server end-to-end suite
- `make test-e2e-local`
  - explicit alias for the same local lane
- `make test-e2e-appliance`
  - reserved for pointing the same client at an already running appliance
- `make check-e2e-logs`
  - re-scan the most recent retained E2E logs without rerunning

The backend module should grow helper targets:

- `make -C services/controlplane test-start`
  - build and start the local server on supplied addresses/data dir
- `make -C services/controlplane test-stop`
  - stop the local test server via pid file
- `make -C services/controlplane test-live`
  - optional backend-owned wrapper for one local live-server scenario

The new `e2etests/` module should grow:

- `make -C e2etests test`
  - run its own package tests/helpers
- `make -C e2etests test-local`
  - run the local integration-test client against a supplied or auto-started server
- `make -C e2etests clean`
  - clean run artifacts

## Verification Chain

The intended repo verification chain is:

1. module unit/contract tests
2. SDK tests
3. local live-server E2E via `make test-e2e`
4. chart static validation

`make verify` should eventually include the local E2E lane once it is stable
and fast enough to be a normal pre-push gate.

Until then, `make test-e2e` may live as an explicit developer target first,
then be promoted into `verify`.

## Ownership Boundary With `appliance-release`

This repo owns:

- public API test client logic
- local live-server orchestration
- source-tree-driven integration scenarios
- SDK-based contract validation

`appliance-release` owns:

- install packaged appliance
- configure target host
- import bundled images
- start K3s and packaged components
- run the same or closely related E2E client against the installed appliance
- collect appliance-level operational evidence

The important boundary is:

- do not duplicate the scenario logic in two repos
- do keep the installation/deployment wrappers in the release repo

## Open Implementation Choices

These are the recommended choices unless we later find a practical blocker:

- create a dedicated top-level `e2etests/` Go module now
- keep the first scenario as a `go run ./local/integration-test`
  executable rather than a shell script
- use the SDK for all REST flows
- use direct HTTP only for protocol-specific `/mcp` validation
- keep the first lane single-process plus external client; do not add K3s
  dependencies to it
- use the backend's fake workflow engine in local E2E for build-flow
  validation

## Execution Sequence

Recommended implementation order:

1. Expand the SDK to cover missing public APIs needed by E2E. Completed for
   current auth/user/role/token/registry/build/developer-workflow REST flows.
2. Add backend `test-start` and `test-stop` targets with isolated env-based
   runtime paths.
3. Create the `e2etests/` module and local server-process helpers.
4. Implement one golden local scenario:
   bootstrap -> login -> token -> user -> role -> permission denial.
5. Add build-flow and registry-flow scenarios.
6. Add developer workflow SDK scenarios for workspaces, target submission,
   jobs, steps, logs, and cancellation.
7. Add live `/mcp` initialize/ping/tools list validation.
8. Add core/storage profile-gating checks for disabled build routes and tools.
9. Add root `make test-e2e`.
10. Promote the lane into `make verify` once stable.
11. Reuse the client in `appliance-release` for installed-appliance checks.

## Non-Goals

This plan does not require:

- local K3s for normal development
- local containers for normal development
- packaging to be present before local E2E exists
- a second machine for the first E2E lane

Those belong to later realism and release-validation lanes, not to the
developer fast path.

# appliance-code

Product repo for the appliance. V1 ships one service, the control plane
(auth, RBAC, HTTP APIs, MCP endpoint, and K3s-facing orchestration), but the
repo is laid out for multiple independently versioned services and their
client SDKs, not as a single-service codebase:

- `server/backend/` — the control-plane service (its own Go module and Makefile)
- `server/sdk/golang/applianceclient/` — a Go client SDK for the control-plane REST API
- `e2etests/` — reserved for external live-server end-to-end test harnesses that use the SDK as a client
- `deploy/charts/appliance-control-plane/` — the control plane's Helm chart (its own Go module, for chart policy tests)

Each has its own `go.mod` and `Makefile`; the root has neither a `go.mod`
nor detailed targets — its `Makefile` only delegates (`make verify`, `make
build`, `make test`, ...) to each module, with one deliberate exception:
`make dev-shell`/`make dev-run` (see [docs/dev-container.md](docs/dev-container.md))
are root-level since they're about the repo as a whole, not any one
module. A `go.work` at the root ties the modules together, including the
shared `../platformkit` module, for local development. Future services
(and their SDKs) are added as new top-level siblings of `server/backend`.

V1 is an offline-first appliance: its sole production distribution is a complete signed air-gap bundle that installs and operates without public internet access.

The current v1 implementation plan is captured in [docs/control-plane-v1-plan.md](docs/control-plane-v1-plan.md).

Accepted architecture decisions and their validation gates are tracked in [docs/decision-register.md](docs/decision-register.md).

The OCI registry product and licensing comparison is in [docs/registry-options.md](docs/registry-options.md).

Phase 0 starts from the dated [compatibility candidates](docs/compatibility-candidates.md) and replaces them with verified release pins.

The production pod and storage layout is shown in the [K3s deployment topology](docs/deployment-topology.md).

The local-first end-to-end testing strategy is captured in [docs/e2e-testing-plan.md](docs/e2e-testing-plan.md).

The accepted ownership and artifact handoff between this private product repo and the public release repo is defined in the [repository boundary](docs/repository-boundary.md).

The control-plane's release container image is built, signed, and scanned only on the Linux build server/CI — never from a developer laptop, and never from macOS regardless of what container tooling is installed there. There is no `make image` target and no Containerfile for the control-plane image in this repo for that reason (see [docs/dev-container.md](docs/dev-container.md)). Day to day, `make build`/`make run`/`make test` work directly against the plain Go binary, no containers involved; `make dev-shell`/`make dev-run` are Linux-only, for interactive debugging in the same toolchain CI uses, not for producing a release artifact.

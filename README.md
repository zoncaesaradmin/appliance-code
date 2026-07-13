# appliance-code

Product repo for the appliance. V1 now ships two always-running product-facing
services: the control plane and a separate server-rendered UI service. The repo
is laid out for multiple independently versioned services and their client SDKs,
not as a single-service codebase:

- `services/controlplane/` — the control-plane service (its own Go module and Makefile)
- `services/ui/` — the HTMX/server-rendered browser UI service (its own Go module and Makefile)
- `sdk/golang/applianceclient/` — a Go client SDK for the control-plane REST API
- `e2etests/` — reserved for external live-server end-to-end test harnesses that use the SDK as a client
- `deploy/charts/appliance-control-plane/` — the appliance chart that now deploys both the control plane and the UI service (its own Go module, for chart policy tests)
- `scripts/package/` — release-input producer helpers for handoff into `appliance-release`

Each has its own `go.mod` and `Makefile`; the root has neither a `go.mod`
nor detailed targets — its `Makefile` only delegates (`make verify`, `make
build`, `make test`, ...) to each module, with one deliberate exception:
`make dev-shell`/`make dev-run` (see [docs/dev-container.md](docs/dev-container.md))
are root-level since they're about the repo as a whole, not any one
module. A `go.work` at the root ties the modules together for local
development. Future services (and their SDKs) are added as new
entries under `services/` and `sdk/`.

`services/controlplane` depends on the shared `github.com/zoncaesaradmin/platformkit`
module (logging, context utilities) as a normal versioned `go.mod`
dependency, vendored into `services/controlplane/vendor/` — no sibling checkout
needed, and builds never require network access. Run `make -C
services/controlplane vendor` after bumping it.

V1 is an offline-first appliance: its sole production distribution is a complete signed air-gap bundle that installs and operates without public internet access.

The current v1 implementation plan is captured in [docs/control-plane-v1-plan.md](docs/control-plane-v1-plan.md).

Accepted architecture decisions and their validation gates are tracked in [docs/decision-register.md](docs/decision-register.md).

The OCI registry product and licensing comparison is in [docs/registry-options.md](docs/registry-options.md).

Phase 0 starts from the dated [compatibility candidates](docs/compatibility-candidates.md) and replaces them with verified release pins.

The production pod and storage layout is shown in the [K3s deployment topology](docs/deployment-topology.md).

The local-first end-to-end testing strategy is captured in [docs/e2e-testing-plan.md](docs/e2e-testing-plan.md).

The accepted ownership and artifact handoff between this private product repo and the public release repo is defined in the [repository boundary](docs/repository-boundary.md).

The control-plane's release container image (`services/controlplane/Containerfile`, built via `make -C services/controlplane image`) is built, signed, and scanned only on the Linux build server/CI, inside the shared dev container (`make dev-shell`) — never from a developer laptop, and never from macOS at all regardless of what container tooling is installed there (see [docs/dev-container.md](docs/dev-container.md) for the exact steps). The runtime image now uses a small Alpine base and defaults to a `debug` runtime profile with common operator tools; set `RUNTIME_PROFILE=runtime` on the image build targets later if you want to cut back to the lean base packages. Day to day `make build`/`make run`/`make test` still work directly against the plain Go binary with no containers involved.

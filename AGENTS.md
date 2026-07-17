# Appliance Development Invariants

These rules apply to all implementation and documentation in this repository.

## Offline-First Product

- V1 has one production package: the complete air-gapped appliance bundle produced by `appliance-release`.
- Installation, startup, normal operation, authentication, builds, registry use, backup, restore, diagnostics, and upgrade must not require public internet access.
- Do not add a connected installer, install-time downloader, remote package repository requirement, phone-home behavior, external license check, dynamic plugin fetch, or background internet updater.
- Every executable, required host package, OCI image, K3s artifact, chart, CRD, scanner database, migration, schema, and static asset required by the released product must be pinned, verified, and present in the signed air-gap bundle.
- Runtime components must remain functional with public egress denied. Tests must exercise this condition.
- Product updates arrive only as new signed offline bundles. Components must not self-update.

## Allowed Network Use

- Clients may access the appliance over its configured local or enterprise network.
- User-directed workflows may access explicitly allowlisted operator-provided services such as an internal Git server, backup target, DNS/NTP service, or future enterprise identity provider. These are deployment inputs, not hidden product dependencies.
- Build definitions must not assume public package repositories or public base-image registries. Required build inputs must be available from zot, the submitted context, or explicitly configured internal allowlists.
- Controlled release assembly and development dependency acquisition may use the internet outside the installed appliance, but those actions must produce pinned, verified bundle inputs and must never occur during installation or runtime.

## Packaging Boundary

- This repo owns product code, the canonical Helm chart, workflow templates, compatibility evidence, and signed immutable release inputs.
- `appliance-release` owns the one complete air-gap bundle and host lifecycle tooling.
- Preserve modular interfaces for future change, but do not add package profiles or alternate connected/offline implementation paths in v1.

## Local Verification Discipline

- Any time you edit this repository, run `make verify` in this repository before considering the work complete.
- Apply this even for small code, chart, workflow, test, Makefile, or documentation changes unless the user explicitly tells you not to run verification.
- If `make verify` fails, fixing that failure becomes the first follow-up task before any further feature work or close-out.
- Do not treat the task as done while `make verify` is failing. Either fix the failure or report the exact blocker and the failing log/location.

## UI To API Observability Contract

- When the browser talks to the UI service and the UI service talks to the control-plane API on the browser's behalf, keep that boundary explicit in code, logs, and documentation.
- Loggers are mandatory runtime dependencies. Service startup or constructor wiring must fail immediately if a required logger is nil instead of silently continuing or creating ad-hoc fallback logger behavior.
- Do not scatter `if logger != nil` or `if Logger != nil` guards through request handling or application flow code. Validate logger presence once at construction/startup time, then use it unconditionally.
- Every UI route that wraps a control-plane REST or MCP call must be documented in an operator-facing mapping document that names:
  - the browser-visible UI route and method
  - the UI handler/function
  - the downstream control-plane method and path
  - the success behavior returned to the browser, such as HTML, fragment HTML, or redirect
- Every new UI route, UI-to-control-plane call, or modification to an existing control-plane API integration must update that mapping document in the same change.
- Runtime observability must let an operator determine whether a browser action stopped at the UI service or reached the control-plane API.
- The UI service and control plane must each write their code-level functional flow logs to their own durable service file under `/var/log/appliance/<service>/application.log`, not only to process stdout/stderr.
- For browser-mediated API flows, runtime logs must show the request that reached the UI or control plane boundary and the response returned there, with enough identifiers to correlate the path across both services.
- Request/response logging for these downstream UI-to-control-plane calls must never leak secrets, bearer tokens, passwords, refresh tokens, or private key material. Redact or omit sensitive fields by default.

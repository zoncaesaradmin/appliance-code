# Developer Workflows V1

This document captures the appliance-native server-side developer workflow
contract that replaces the old standalone ForgeLine server runtime inside the
Zon appliance. ForgeLine remains the behavioral reference, but the appliance
control plane owns the API, MCP tools, RBAC checks, durable state, and workflow
submission.

## Architecture

- The `builder` appliance profile enables the `build` capability and registers
  developer workflow REST routes and MCP tools.
- `core` and `storage` profiles do not register workspace/job/build-target
  developer workflow routes; callers receive the normal appliance 404.
- The control plane stores catalog/config, workspace profile metadata, workspace records,
  user-scoped current workspace selection, jobs, and job steps in SQLite.
- Build target submission resolves `current workspace + build target + tag` into
  the existing immutable build request shape.
- Build execution uses the workflow engine interface. Local tests use the fake
  workflow engine, while production builder-profile deployments use the Argo
  adapter through Kubernetes API calls.
- V1 build targets are still ephemeral clone-per-build: the control-plane API
  stores workspace metadata, then the workflow pod clones the configured source
  at the immutable commit and executes the target's structured execution mode.
- Build and job records keep the immutable source commit plus the resolved
  target artifact reference so REST and MCP clients can track the submitted
  image identity directly.
- Supported execution modes are `repo_script` and `make_target`. `repo_script`
  runs the configured script path, defaulting to `build.sh`, with build context
  environment variables such as `TARGET_IMAGE` and `CONTAINERFILE_PATH`.
  `make_target` runs the configured make target with the same structured
  variables. `scriptPath` and `containerfilePath` must be clean relative paths
  inside the cloned repo; absolute paths, backslashes, and `.` or `..` path
  segments are rejected.
  Direct low-level build submissions without an execution mode keep the default
  containerfile/buildah behavior.

The build-catalog and API field names keep ForgeLine-compatible keys such as
`workProfiles`, `workProfile`, and `work_profile`. In user-facing wording, these
mean `workspace profile` to distinguish them from the product-facing
`appliance profile`.

## Git Sources And Credentials

Catalog repos may use HTTPS or SSH-style Git URLs, including
`git@host:org/repo.git`. All forms are still checked against the configured Git
host allowlist. SSH repos must declare a source credential reference, and that
credential must include both the private-key Secret and a pinned `known_hosts`
Secret.

Source credentials are represented only as opaque catalog references to
appliance/Kubernetes secrets. Private key material must not appear in product
config, release bundles, API responses, MCP responses, logs, or command
arguments. Production Argo workflows mount the selected credential secret and
its pinned `known_hosts` secret read-only into the short-lived workflow pod that
performs `git clone`; the control-plane pod never shells out with or stores the
key material.

## RBAC

The RBAC model is unchanged: capabilities gate route/tool registration, and
permissions authorize the authenticated principal. Both checks must pass.

- `developer` receives REST developer workflow permissions and `mcp.invoke`.
- `automation` receives REST developer workflow permissions but does not receive
  `mcp.invoke`; automation remains REST-only by default.
- MCP `tools/list` requires `mcp.invoke`.
- MCP `tools/call` requires `mcp.invoke` plus the underlying operation
  permission.

## Implementation Evidence

The service/API/MCP contract is covered with fake-workflow tests so local
validation does not require a live K3s/Argo environment. Production
builder-profile deployments use the Argo workflow engine when
`workflowEngine` is `argo`.

Covered by tests:

- catalog validation, duplicate workspace-profile aliases, and source credential host matching;
- SQLite workspace/current-workspace/job/job-step persistence;
- builder startup requires a valid build catalog;
- SSH and HTTPS Git source URL validation with fail-closed host allowlists;
- builder profile exposes workspace/build-target/job REST flows;
- core profile hides developer workflow REST routes;
- builder profile lists developer workflow MCP tools;
- core profile hides developer workflow MCP tools;
- core profile rejects direct developer workflow MCP tool calls as
  tool-not-found;
- `mcp.invoke` alone cannot submit a build;
- build permissions alone cannot use MCP;
- REST and MCP make the same allow/deny decision for a developer workflow
  operation when the same principal has or lacks the underlying operation
  permission;
- workflow status messages and logs returned through REST/MCP are redacted for
  configured source credential Secret names and private-key PEM blocks;
- Argo workflow submission/status/log/cancel behavior through the Kubernetes
  API adapter;
- local live-server e2e starts in builder profile with a valid build catalog
  and verifies the developer workflow REST SDK surface plus the initial builder
  MCP tool surface through `/mcp`;
- local live-server e2e starts in core and storage profiles and verifies
  developer workflow REST routes return `404`, MCP build tools are absent from
  `tools/list`, and direct disabled build tool calls return tool-not-found;
- Helm rendering for builder-profile Argo RBAC and service account token
  mounting;
- release-input/bundle plumbing for extra pinned OCI images used by builder
  workflow pods.

## Deferred

- Persistent workspace PVCs; V1 starts with ephemeral clone-per-build semantics.
- Admin-enabled server-local host-path workspaces. If added later, they must be
  explicit product config with allowlisted base paths, symlink escape checks,
  and workflow-pod mounts; the control-plane pod must not implicitly use host
  `~/.ssh` or arbitrary host filesystem paths.
- Server-side deploy tools.
- ForgeLine bridge appliance mode.
- Product black-box build conformance against a live air-gapped K3s install.

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
- A workspace is one logical developer workspace selected from one workspace
  profile. It is not one workspace per repo. The chosen workspace profile
  determines the set of repos materialized inside that workspace.
- Build target modeling is intentionally separate from workspace creation. A
  repo can have zero, one, or many build targets, so workspace creation must
  not require output image repositories or per-target builder images.
- Install-time catalogs may include optional build targets. Prefer nesting
  them under `repos[].buildTargets`. `zonctl install` / `zonctl upgrade` and
  the control plane flatten nested targets into the runtime `buildTargets`
  list (filling each target's `repo` from its parent) and inject the catalog
  into `config.buildCatalog`. Top-level `buildTargets` with an explicit
  `repo` field remain accepted. Optional per-target `builderImageDigest`
  defaults to the short bundle name `automation-dev` (also accepted when
  omitted). The control plane resolves that name to
  `config.builderImageDigest`, which zonctl injects from the signed bundle's
  packaged automation-dev OCI image. Advanced catalogs may override with a
  digest-pinned reference that is present in the bundle; users should not
  paste floating GHCR tags.
- Target mapping is name/alias → one catalog entry → one execution policy.
  One repo may expose many targets. A target whose `name` equals the repo
  name is still an explicit mapping (typically `execution: make` with
  `args: [build]`); it does not expand ForgeLine-style `build_components`.
  Supported executions are `make` and `script`. Mode input is the `args` list
  (v1: exactly one entry). Legacy `make_target`/`repo_script` plus
  `makeTarget`/`scriptPath` are normalized into this shape at load time.
- Current-workspace build submission resolves
  `current workspace + build target + tag` and builds the on-disk tree at
  `/data/zon/workspaces/<workspace-name>/<repo-name>/`. Builds do not clone
  Git and do not use `repos[].defaultRef`. `defaultRef` is only for workspace
  prepare/sync (branch names are fine). Other workflows may refresh that
  workspace tree; the build workflow builds whatever is present at submit time.
  When `imageTag` is omitted, the default is `{workspace}-{target}` unless the
  catalog `imageTagTemplate` supplies another value using `{workspace}` /
  `{target}`.
- Build execution uses the workflow engine interface. Local tests use the fake
  workflow engine, while production builder-profile deployments use the Argo
  adapter through Kubernetes API calls.
- V1 workspaces are materialized onto the shared workspace PVC under the fixed
  host-visible root `/data/zon/workspaces/<workspace-name>`. Current-workspace
  builds run against the repo directories already present under that workspace;
  they do not re-clone. Workspace ownership follows the
  fixed numeric identity and shared filesystem group rules in
  [workload identity and storage security](workload-identity-and-storage-security.md).
- Operators can override the host-visible workspace root with the chart value
  `workspaceStorage.rootDir`, but ordinary installs should use the default
  `/data/zon/workspaces` so workspace source trees stay separate from
  `zonctl` control state.
- Build and job records keep the resolved target artifact reference so REST and
  MCP clients can track the submitted image identity. Workspace builds record
  `workspace-local` rather than a catalog commit SHA.
- Supported execution modes are `script` and `make`. `script` runs the path in
  `args[0]`, defaulting to `build.sh` when legacy catalogs omit args, with build
  context environment variables such as `TARGET_IMAGE` and `CONTAINERFILE_PATH`.
  `make` runs `make <args[0]>` with the same structured variables.
  `args` entries used as script paths, and `containerfilePath`, must be clean
  relative paths inside the workspace repo directory; absolute paths,
  backslashes, and `.` or `..` path segments are rejected.
  Prefer `execution: make` with a real Makefile target when the repo has one
  (for example forgeline `build`, not a bare root `build.sh`). Script args must
  name a file that exists in the checkout (for example `scripts/build.sh`).
  Direct low-level build submissions without a workspace still use the default
  containerfile/buildah clone path.

Build workflow pods override builder-image home/cache paths (`HOME`, `GOPATH`,
`GOCACHE`, `GOMODCACHE`, and related vars) to writable directories under
`/tmp/appliance-home` so non-root workflow UIDs are not blocked by image ENV
values such as `/home/vscode/go`.

The build-catalog and API field names keep ForgeLine-compatible keys such as
`workProfiles`, `workProfile`, and `work_profile`. In user-facing wording, these
mean `workspace profile` to distinguish them from the product-facing
`appliance profile`.

## Git Sources And Credentials

Catalog repos must use HTTPS Git URLs. Repo hosts are still checked against the
configured Git host allowlist. Private keys, SSH-only credential metadata,
tokens, and passwords do not belong in product config,
release bundles, API responses, MCP responses, logs, or command arguments.

The appliance now keeps one shared builder Git HTTPS credential as runtime
state in the `appliance-builds` namespace rather than in the build catalog.

- The control plane exposes `GET /api/v1/builder/git-access` so the UI can
  discover whether that shared credential exists and which catalog host it is
  expected to cover.
- Administrators configure or rotate it through
  `PUT /api/v1/builder/git-access`.
- Workspace creation and workspace prepare fail closed with
  `412 Precondition Failed` until that shared credential exists.
- Argo workspace-prepare workflow pods mount the resulting Kubernetes Secret and
  use `GIT_ASKPASS` (not interactive prompts or brittle `http.extraHeader`
  config) for HTTPS `git clone` calls. Current-workspace build workflows do
  not clone and do not require that credential.
- The Git host must match the catalog (for example `github.com`). Username is
  typically the Git forge username or `x-access-token`; the token must be a
  forge personal access token that can read every repo in the workspace
  profile. Appliance login usernames/passwords are not Git credentials.

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

- catalog validation, duplicate workspace-profile aliases, and HTTPS Git host allowlists;
- SQLite workspace/current-workspace/job/job-step persistence;
- builder startup requires a valid build catalog;
- HTTPS Git source URL validation with fail-closed host allowlists;
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
  configured sensitive markers and private-key PEM blocks;
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

- Runtime/UI-managed alternate workspace storage roots. V1 keeps the ordinary
  builder workspace PVC mounted at `/data/zon/workspaces` unless an operator
  overrides `workspaceStorage.rootDir` at install/configuration time. If
  additional per-user or per-workspace storage roots are added later, they must be
  explicit product config with allowlisted base paths, symlink escape checks,
  and workflow-pod mounts; the control-plane pod must not implicitly use host
  `~/.ssh` or arbitrary host filesystem paths.
- Server-side deploy tools.
- ForgeLine bridge appliance mode.
- Product black-box build conformance against a live air-gapped K3s install.

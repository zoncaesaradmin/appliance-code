# Appliance Metadata Bundle, Profiles, And Licensing Plan

## Purpose

This document replaces the earlier custom-profile workflow with the new
appliance metadata-bundle model.

The previous implementation direction allowed administrators to create,
update, and delete custom appliance profiles through the control-plane API and
UI. The revised direction removes that user-authored profile workflow.
Profiles are now vendor-defined product policy, delivered through a signed
appliance metadata bundle. Administrators can update or roll back the active
metadata bundle, view the profiles it provides, validate a profile, and activate
an eligible profile.

This keeps profile behavior flexible without turning appliance administrators
into profile-rule authors.

## Product Direction

- Installation remains simple and offline-first.
- If no install profile is provided, the installer selects the default base
  profile.
- The default base profile id remains `core`; UI display text may show
  `Base (core)`.
- Licensing is never requested during installation.
- First login shows an unresolved-licensing alert until an administrator
  imports an offline license or explicitly accepts the base/free entitlement.
- Profiles come from the active signed appliance metadata bundle.
- Administrators do not create or edit profile definitions through the product
  API or UI.
- Profile activation is still a post-install administrator workflow.
- Policy-bundle updates are the mechanism for adding or changing product
  profiles, capability rules, capability licensing requirements, and related
  product policy.
- A metadata bundle can change behavior only when the installed software already
  contains the required engine support, APIs, services, charts, images, host
  packages, migrations, and runtime artifacts.
- Any change requiring new code or new artifacts still requires a full
  appliance software bundle update.

## Definitions

- `software bundle`: the complete signed air-gapped appliance release bundle
  containing binaries, images, charts, migrations, static assets, and the
  policy engine.
- `appliance metadata bundle`: a signed product-policy input compatible with one
  exact software version. It may contain profile definitions, capability
  rules, capability licensing requirements, validation rules, UI visibility
  metadata, warning text, defaults, and operational guardrails.
- `metadata version`: a four-part version. The first three parts match the exact
  software version, and the fourth part is the metadata-only revision.
- `built-in/base metadata bundle`: the metadata bundle shipped with the software
  bundle, normally ending in `.0`.
- `metadata-bundle patch`: a later vendor-signed metadata bundle for the same
  software version, normally ending in `.1`, `.2`, and so on.
- `profile`: a vendor-defined appliance operating mode from the active metadata
  bundle.
- `capability`: a functional building block such as `base`, `host`,
  `workflows`, `build`, `artifact`, or `dns`.
- `license unresolved`: the appliance has not yet been explicitly configured
  with a license file or accepted base/free entitlement.
- `base/free entitlement`: the local fallback entitlement accepted by the
  administrator when no fuller license is applied.
- `profile activation`: the administrative operation that changes the active
  appliance profile after validating policy, bundle artifacts, license
  entitlement, and current appliance state.

## Versioning Contract

Policy-bundle compatibility is intentionally strict.

```text
Software version:      4.3.2
Base metadata version:   4.3.2.0
Patch metadata version:  4.3.2.1
Next metadata patch:     4.3.2.2
```

Rules:

1. Software `4.3.2` accepts only metadata bundles `4.3.2.N`.
2. Software `4.3.2` rejects metadata bundles `4.3.1.N`, `4.3.3.N`, and
   `4.4.0.N`.
3. The first three metadata-version segments must exactly match the running
   appliance software version.
4. The fourth segment is the metadata-only revision and must be a non-negative
   integer.
5. Full software upgrades carry their matching base metadata bundle.
6. Policy-only updates are allowed only within the same exact software
   version.
7. Rollback is allowed only to a previously accepted metadata bundle for the
   same software version.

Example metadata:

```yaml
apiVersion: metadata.zon/v1
kind: ApplianceMetadataBundle
metadata:
  softwareVersion: 4.3.2
  metadataVersion: 4.3.2.1
  createdAt: "2026-08-03T00:00:00Z"
  vendor: zon
```

This versioning model keeps support simple:

```text
Running software: 4.3.2
Active metadata:    4.3.2.1
```

## Metadata Bundle Scope

Profile policy is the first consumer, but the bundle should be designed as an
appliance-level metadata bundle rather than a profile-only document.

Initial metadata-bundle directories may include:

- `profiles/` for profile catalog documents
- `capabilities/` for capability catalog, dependency/conflict rules, required
  artifact references, and license entitlement requirements
- `ui/` for UI visibility metadata and product text
- `notifications/` for setup and alert policy
- `activation/` for profile activation warnings and transition policy
- `mcp-tools/` for future MCP tool descriptors, workflow-backed tool policy,
  and tool visibility rules

Example content shape:

```text
appliance-metadata-bundle-4.3.2.1/
  bundle.yaml
  profiles/
    catalog.yaml
  capabilities/
    catalog.yaml
    rules.yaml
  activation/
    transitions.yaml
    warnings.yaml
  ui/
    visibility.yaml
    messages.yaml
  notifications/
    alerts.yaml
  mcp-tools/
    README.md
```

Capability-local policy keeps the model simple. A capability owns the rules
needed to decide whether it can be activated:

```yaml
capabilities:
  build:
    displayName: Build
    requires: [base, host, workflows, artifact]
    conflicts: []
    license:
      requiredEntitlement: zon.capabilities.build
    artifacts:
      required:
        - workflow-controller
        - build-task-image

  dns:
    displayName: LAN DNS
    requires: [base, host]
    license:
      requiredEntitlement: zon.capabilities.dns
    artifacts:
      required:
        - coredns-chart
```

Do not add a separate top-level `entitlements/` directory in v1. Licensing
requirements are capability policy. A future top-level licensing policy area
can be added only if licensing grows into an independent product domain with
SKUs, editions, feature tiers, or cross-capability override logic.

The control-plane implementation should keep the engine typed and constrained
first. Do not introduce arbitrary administrator-authored CEL, Rego, scripts,
or a Turing-complete YAML language in the first implementation.

If typed rules later become too limiting, add a narrow expression mechanism
only for vendor-signed bundle content and only after compatibility, debugging,
and support tooling are ready.

## Product Rules

1. Installation must not ask for or require a license.
2. Installation must not require internet access for licensing or entitlement
   checks.
3. If no install profile is specified, the installer must select `core`.
4. Install-time profile choices must be limited to profiles in the software
   bundle's built-in/base metadata bundle.
5. Post-install profile choices must be limited to profiles in the active
   signed metadata bundle.
6. Administrators must not create, update, or delete profile definitions
   through the product API or UI.
7. A metadata-bundle update is required to add, remove, or change product
   profiles.
8. The UI must show a licensing alert after login while the license state is
   unresolved.
9. Resolving the license state means either importing a valid offline license
   or explicitly accepting the base/free entitlement.
10. Profile listing may be visible before licensing is resolved, but profile
    activation must remain unavailable until licensing is resolved.
11. Policy-bundle install must fail closed if the bundle signature, version,
    schema, profile rules, artifact references, or compatibility checks fail.
12. Profile activation must fail closed if the requested profile is invalid,
    references missing local artifacts, is not licensed, or cannot transition
    safely from current appliance state.
13. No workflow may download missing profile content, policy content, license
    content, images, charts, host packages, or plugins at install time or
    runtime.

## Target Lifecycle

```text
Install starts
  -> installer reads selected profile, if any
  -> no profile selected means core
  -> installer deploys complete signed offline software bundle
  -> built-in metadata bundle 4.3.2.0 becomes active
  -> first admin bootstrap completes
  -> UI login succeeds
  -> notification alert shows "Licensing is not configured"
  -> admin opens Admin / Licensing
  -> admin imports offline license or accepts base/free entitlement
  -> license state becomes resolved
  -> licensing alert clears
  -> admin may view metadata bundle and profiles
  -> admin may install metadata bundle 4.3.2.1
  -> admin may validate and activate an eligible profile from active metadata
```

## Install-Time Workflow

The installer should accept an optional profile value from the built-in/base
metadata bundle included in the software bundle.

Expected behavior:

- If the operator specifies a built-in profile, use it.
- If the operator omits the profile, use `core`.
- If the operator specifies an unknown profile, fail before mutating the
  target.
- If the specified profile requires capabilities or artifacts not present in
  the signed software bundle, fail closed.
- Do not ask for a license file.
- Do not ask the operator to choose or edit capabilities.
- Do not expose metadata-bundle editing during install.

The install-time UX should stay close to:

```text
Selected appliance profile: core
Metadata bundle: 4.3.2.0
Licensing: not configured; complete licensing setup after first login
```

## First Login And Licensing Alert

After login, the UI shell should load appliance setup state from the control
plane. If licensing is unresolved, the top-right notification bell shows an
active alert count.

Initial alert:

```text
Licensing is not configured
Configure licensing to unlock entitled capabilities, or continue with the
base entitlement.
```

Behavior:

- The alert appears immediately after login.
- The alert is part of system notifications, not a modal that blocks reading
  the home dashboard.
- The alert remains active until the license state is resolved.
- Users without licensing administration permission can see that licensing is
  unresolved but cannot change it.
- Administrators can click through to Admin / Licensing.
- Once an admin imports a license or accepts base/free entitlement, the alert
  clears.

`license unresolved` and `base/free active` are different states. `base/free
active` is a deliberate configured state, not an incomplete setup.

## Licensing Setup Workflow

Admin / Licensing should support two resolution paths.

### Import Offline License

The administrator uploads or pastes an offline license document.

Validation should check:

- license document format
- signature and issuer trust
- appliance or customer binding, if applicable
- validity window
- licensed capabilities and feature entitlements
- license version compatibility with the running appliance software version

On success:

- store the accepted license state
- audit the event
- clear the unresolved-license alert
- recompute visible capabilities, feature flags, and eligible profiles

### Accept Base/Free Entitlement

The administrator explicitly accepts the base/free entitlement when no fuller
license is available.

On success:

- store the base/free entitlement state
- audit the event
- clear the unresolved-license alert
- keep advanced capabilities unavailable until a fuller license is imported

## Metadata Bundle Management Workflow

Admin / Metadata Bundle should expose the active metadata bundle and safe update
operations.

UI flow for install/update:

1. Administrator opens Admin / Metadata Bundle.
2. UI shows running software version, active metadata version, and previous
   retained metadata version if one exists.
3. Administrator uploads a signed metadata bundle file.
4. Backend validates signature, version compatibility, schema, policy rules,
   artifact references, and current license compatibility.
5. UI shows validation results and impact summary.
6. Administrator confirms install.
7. Backend stores the new metadata bundle, marks it active, audits the event,
   and recomputes profile catalog visibility.

UI flow for rollback:

1. Administrator opens Admin / Metadata Bundle.
2. UI shows the previous retained metadata bundle.
3. Administrator requests rollback.
4. Backend validates the rollback target still matches the running software
   version.
5. Administrator confirms rollback.
6. Backend restores the previous metadata bundle, audits the event, and
   recomputes profile catalog visibility.

Rules:

- Metadata bundles are vendor-signed product inputs.
- Operators can install or roll back metadata bundles, not edit them in place.
- At least the active metadata bundle and one previous metadata bundle should be
  retained locally for rollback.
- Policy-bundle changes must be included in backup and restore.
- Support bundles must include active metadata, validation results, and
  effective policy summaries, but not secret license material.
- Installing a metadata bundle must not activate a different appliance profile
  automatically unless a future policy explicitly supports a safe migration
  flow. The default should be no implicit profile activation.

## Profile Catalog And Activation Workflow

Admin / Profiles should become a view-and-activate surface over the active
metadata bundle.

Administrators can:

- list profiles from the active metadata bundle
- inspect a profile's capabilities, required artifacts, entitlement needs, and
  warnings
- validate whether a profile can be activated
- activate an eligible profile after confirmation

Administrators cannot:

- create arbitrary new profile definitions
- edit vendor profile capability mappings
- delete vendor profile definitions
- bypass metadata-bundle validation

Activation flow:

1. Administrator selects a profile from the active metadata bundle.
2. UI requests activation validation.
3. Backend returns validation results grouped by policy, artifact availability,
   license entitlement, and transition safety.
4. UI shows a clear impact summary and warnings.
5. Administrator confirms activation.
6. Backend stores desired/active profile state and starts the required
   reconcile or restart path.
7. UI shows progress, final success, or actionable failure.

Validation must include:

- profile exists in the active metadata bundle
- requested capabilities are known to the installed software
- capability dependencies and conflicts are satisfied
- required bundle artifacts are present locally
- required bundle artifacts match the signed software manifest
- license entitlement allows every requested capability
- transition from current profile to target profile is supported

Activation must fail closed if validation fails.

## Bundle Availability Checks

Policy-bundle install and profile activation must prove that required local
release inputs exist. They must not only validate capability names.

The backend should validate required artifacts against the signed software
bundle manifest, such as:

- container images
- Helm charts
- workflow templates
- host packages
- migration assets
- configuration templates
- scanner or auxiliary databases, where applicable

If a profile is valid and licensed but the required software-bundle artifact
is not available locally, activation must fail with a clear error:

```text
Cannot activate profile "builder-custom": required artifact
"workflow-task-image" is not present in the installed software bundle.
```

No activation path may fetch missing artifacts from the internet or from a
remote package repository.

## Metadata Bundle Artifact And Bundle Assembly Contract

The appliance metadata bundle must be a distinct release artifact, even when it
is packaged inside the larger complete appliance bundle.

The canonical metadata bundle content is a directory tree. The `.tar.zst`
artifact is only the signed and digest-pinned transport form of that directory.
Designing the bundle as a directory from the start lets later policy areas,
such as MCP tool descriptors or workflow-backed tool policy, be added without
changing the metadata-bundle artifact model.

There are two bundle layers:

```text
appliance software release-input
  -> control-plane image
  -> UI image
  -> Helm chart
  -> configuration schema
  -> appliance-metadata-bundle-4.3.2.0.tar.zst
     contains appliance-metadata-bundle-4.3.2.0/
  -> compatibility, SBOM, provenance, notices, tests

final appliance air-gap bundle
  -> release-manifest.json
  -> release-manifest.sig
  -> artifacts/appliance-metadata-bundle-4.3.2.0.tar.zst
     contains appliance-metadata-bundle-4.3.2.0/
  -> every other install/runtime artifact
```

The base metadata bundle shipped with software `X.Y.Z` must be generated as a
directory and then emitted as a compressed archive, normally:

```text
appliance-metadata-bundle-X.Y.Z.0/
  bundle.yaml
  profiles/
  capabilities/
  activation/
  ui/
  notifications/
  mcp-tools/

appliance-metadata-bundle-X.Y.Z.0.tar.zst
```

The metadata-bundle directory must not be flattened into the control-plane image,
Helm chart, or configuration schema as the only copy. Those components may
embed or mount a copy for runtime convenience, but the release workflow still
treats the compressed metadata-bundle directory as a first-class artifact with
its own path, size, digest, signature or manifest signature coverage,
SBOM/provenance evidence where applicable, and release-input metadata.

Minimum v1 directory contract:

- `bundle.yaml` is required and declares bundle identity, software version,
  metadata version, schema version, creation metadata, and included sections.
- `profiles/catalog.yaml` is required for v1 because profile policy is the
  first consumer.
- `capabilities/catalog.yaml` is required for v1 and owns capability
  dependencies, conflicts, required artifacts, and license entitlement
  requirements.
- Other directories may contain placeholder `README.md` files at first, but
  the generator should create the stable directory structure now.
- Unknown files are rejected by default unless the metadata-bundle schema
  explicitly marks the directory as extension-tolerant.
- Every file in the directory contributes to the metadata-bundle archive digest.
- Internal file paths must be relative, deterministic, and target-independent.

Required `appliance-code` build outputs:

- generate the built-in/base metadata-bundle directory from product policy
  sources
- name the metadata bundle using the four-part metadata version
- package the directory as `appliance-metadata-bundle-X.Y.Z.0.tar.zst`
- validate the metadata bundle against the running software version
- include the metadata bundle in `release-input.json`
- include the metadata bundle in release-input checksums
- include metadata-bundle validation tests in release-input conformance tests
- fail the product build if the base metadata bundle is missing, malformed, or
  not compatible with the software version

Required `appliance-release` bundle assembly behavior:

- consume the metadata-bundle artifact from release-input
- copy the metadata-bundle archive into the final extracted appliance bundle as
  a separate artifact
- record the metadata-bundle path, size, digest, metadata version, and compatible
  software version in the final signed release manifest
- verify the metadata-bundle digest during final bundle verification
- reject a release input whose metadata bundle is missing or whose metadata does
  not match the product version
- keep the final bundle portable by avoiding target-specific metadata-bundle
  content

Required `appliance-ctl` install behavior:

- verify the final release manifest and metadata-bundle artifact before
  installation uses it
- extract or stage the base metadata-bundle directory as the initial active
  metadata bundle
- pass the selected install profile only after validating it against the
  verified base metadata bundle
- record installed metadata-bundle version and digest in installed state
- include active metadata-bundle metadata in `status`, `verify`, diagnostics,
  backup, restore, and support-bundle flows

Policy-only updates after installation may be distributed as standalone signed
metadata-bundle directory archives for the exact same software version, but every
full software release must still carry its base `.0` metadata-bundle directory
archive as a separate artifact inside the complete appliance bundle.

## API Surface Plan

The exact OpenAPI shape should be finalized during implementation. The
important contract change is that profile CRUD is removed and replaced by
metadata-bundle management plus read-only profile catalog and activation.

Licensing:

- `GET /api/v1/licensing/status`
- `PUT /api/v1/licensing/license`
- `POST /api/v1/licensing/base-entitlement/accept`
- `GET /api/v1/licensing/entitlements`

Notifications:

- `GET /api/v1/notifications`
- `POST /api/v1/notifications/{id}/acknowledge`

Metadata bundle:

- `GET /api/v1/appliance/metadata-bundle`
- `POST /api/v1/appliance/metadata-bundle/validate`
- `POST /api/v1/appliance/metadata-bundle/install`
- `POST /api/v1/appliance/metadata-bundle/rollback`

Profiles:

- `GET /api/v1/appliance/profiles`
- `GET /api/v1/appliance/profiles/{profileId}`
- `POST /api/v1/appliance/profiles/{profileId}/validate`
- `POST /api/v1/appliance/profiles/{profileId}/activate`
- `GET /api/v1/appliance/capabilities`

Setup state:

- `GET /api/v1/appliance/setup-state`

Remove from the previous custom-profile API:

- `POST /api/v1/appliance/profiles`
- `PUT /api/v1/appliance/profiles/{profileId}`
- `DELETE /api/v1/appliance/profiles/{profileId}`

`setup-state` should let the UI answer:

- active profile id
- desired profile id, if a profile activation is pending
- active metadata-bundle version
- previous metadata-bundle version, if rollback is available
- whether licensing is unresolved
- whether profile activation is available
- whether metadata-bundle management is available
- whether any blocking setup actions remain
- which notification alerts should be shown immediately

## Permissions Plan

Initial permissions should remain small and explicit.

Suggested permission families:

- `licensing.read`
- `licensing.manage`
- `metadata.read`
- `metadata.manage`
- `profiles.read`
- `profiles.activate`
- `notifications.read`
- `notifications.acknowledge`

Remove or avoid:

- `profiles.manage` for profile CRUD

The system administrator role should receive all of these permissions.
Non-admin roles may receive read-only notification visibility where
appropriate.

## Current Code Assessment

The codebase already contains parts of the previous plan:

- licensing status, offline license import, and base/free entitlement services
- unresolved-license notification behavior
- setup-state API fields for licensing and profile-management availability
- profile listing, validation, activation, and custom profile CRUD handlers
- SQLite storage for custom profiles and activation state
- OpenAPI paths for profile create/update/delete

The metadata-bundle change should refactor rather than duplicate this work.

Keep or adapt:

- licensing state model
- notification service
- setup-state API concept
- profile validation result grouping
- profile activation confirmation model
- capability dependency validation
- bundle-artifact validation concept
- audit recording for licensing/profile changes

Remove or replace:

- custom profile create/update/delete API handlers
- custom profile write request schema
- custom profile SQLite table usage as the source of product profiles
- UI create/edit/delete profile forms
- tests that assert administrator-created custom profiles are supported
- wording that says administrators create new profiles directly

Add:

- metadata-bundle metadata model
- metadata-bundle signature and version validation
- active/previous metadata-bundle persistence
- metadata-bundle parser and typed validation engine
- metadata-bundle install and rollback APIs
- metadata-bundle Admin UI page
- read-only profile catalog derived from the active metadata bundle

## UI Deliverables

The React control-plane UI should add or update:

- notification bell fed by the notifications API
- unresolved-license alert in the notification panel
- Home dashboard setup card for unresolved licensing
- Admin / Licensing page
- Admin / Metadata Bundle page
- Admin / Profiles page as read-only catalog plus activation workflow
- profile validation result component
- metadata-bundle validation result component
- activation confirmation component
- locked state for profile activation when licensing is unresolved

The UI should remove:

- create profile form
- edit profile form
- delete profile action
- custom capability selection workflow

The UI should continue using the existing shell pattern:

- left rail for Home, Manage, Analyze, and Admin
- transient feature selectors for Manage, Analyze, and Admin
- page-level tabs inside each feature page
- Tailwind-based reusable components

## Control Plane Deliverables

Backend work should add or refactor:

- metadata-bundle directory schema with `bundle.yaml`,
  `profiles/catalog.yaml`, and `capabilities/catalog.yaml`
- metadata-bundle signature verification
- exact software-version compatibility check
- metadata-version ordering and rollback validation
- active and previous metadata-bundle persistence
- metadata-bundle install/rollback service
- profile catalog loader backed by active metadata bundle
- capability-policy loader backed by active metadata bundle
- profile activation validator backed by active metadata bundle and
  capability-local license/artifact rules
- metadata-bundle validation result schema
- audit events for metadata-bundle install and rollback
- OpenAPI updates
- route and permission registration
- tests for every fail-closed validation path

Backend work should remove or deprecate:

- custom profile create/update/delete service methods
- custom profile storage methods if no other state requires them
- custom profile migrations from the active code path, with a safe migration
  decision for existing development databases
- profile-management permission semantics tied to CRUD

The first implementation may store metadata-bundle state in SQLite, matching
the current v1 persistence direction.

## Installer And Release Deliverables

`appliance-ctl` and `appliance-release` should align with this model:

- `appliance-code` emits a separate metadata-bundle artifact in release-input
- installer defaults to `core` when profile is omitted
- installer accepts only profiles from the built-in/base metadata bundle
- installer does not prompt for licensing
- installer does not validate license entitlement
- software bundle includes base metadata-bundle directory archive
  `softwareVersion.0` as a separate artifact, not only embedded into another
  artifact
- final appliance bundle includes that same metadata-bundle directory archive as
  a separately listed manifest artifact
- release manifest exposes enough artifact metadata for metadata-bundle and
  profile-activation validation
- metadata-bundle artifacts are signed, checksummed, versioned, and included in
  bundle metadata
- release verification checks metadata-bundle version rejection and rollback
- install summary tells the operator to complete licensing after first login

## Execution Phases

### Phase 1: Contract Update And API Cutover

Deliverables:

- update product docs to say profiles are vendor-defined through policy
  bundles
- remove planned custom profile CRUD from OpenAPI and UI implementation plan
- add metadata-bundle API plan and versioning contract
- decide handling of existing custom-profile development data
- define the canonical v1 metadata-bundle directory layout
- remove separate top-level `entitlements/` and `artifacts/` policy areas from
  the v1 plan
- define `capabilities/catalog.yaml` as the owner of dependency, conflict,
  license entitlement, and required-artifact rules

Validation gate:

- docs clearly state no administrator-authored profile definitions in v1
- no workflow suggests runtime internet access or connected policy checks
- OpenAPI diff intentionally removes profile CRUD and adds metadata-bundle
  operations
- metadata-bundle examples show a directory tree, not a single flat policy file

### Phase 2: Appliance-Code Metadata Bundle Source And Generator

Deliverables:

- add product policy source files under a stable source directory, for example
  `metadata-bundle/base/`
- create a generator that emits
  `appliance-metadata-bundle-X.Y.Z.0/`
- generate required directories:
  `profiles/`, `capabilities/`, `activation/`, `ui/`, `notifications/`, and
  `mcp-tools/`
- generate `bundle.yaml` with software version, metadata version, schema
  version, creation metadata, and included sections
- generate `profiles/catalog.yaml`
- generate `capabilities/catalog.yaml` with dependencies, conflicts,
  required artifacts, and license entitlement requirements
- package the directory as `appliance-metadata-bundle-X.Y.Z.0.tar.zst`
- make the generator deterministic so repeated builds produce stable content
  for the same inputs

Validation gate:

- generated archive contains exactly one top-level directory named
  `appliance-metadata-bundle-X.Y.Z.0`
- required files exist
- no target-specific hostnames, IPs, URLs, or credentials appear in the bundle
- generated content is deterministic for unchanged inputs

### Phase 3: Metadata Bundle Schema, Parser, And Validator

Deliverables:

- define typed Go structs for `bundle.yaml`, `profiles/catalog.yaml`, and
  `capabilities/catalog.yaml`
- implement archive extraction with path traversal protection
- implement schema validation for all required files
- validate exact software-version match and four-part metadata version
- validate profile ids, profile capability references, capability ids,
  dependencies, conflicts, license entitlement keys, and artifact references
- validate unknown files/directories according to explicit extension rules
- validate that all profile capabilities exist in `capabilities/catalog.yaml`
- validate that all required artifacts can be mapped to signed release
  manifest entries or known software-bundle artifact ids
- load the built-in/base metadata bundle at startup

Validation gate:

- software `4.3.2` accepts `4.3.2.0` and `4.3.2.1`
- software `4.3.2` rejects `4.3.1.N`, `4.3.3.N`, and `4.4.0.N`
- unknown capability references fail closed
- missing required files fail closed
- archive path traversal attempts fail closed

### Phase 4: Release-Input Artifact Production

Deliverables:

- include `appliance-metadata-bundle-X.Y.Z.0.tar.zst` in `release-input/`
- add metadata-bundle metadata to `release-input.json`: archive path, digest,
  size, top-level directory name, software version, metadata version, schema
  version, and validation evidence
- add the archive to release-input checksums
- add product conformance tests that validate the generated metadata bundle
- fail the `appliance-code` release-input build when metadata-bundle generation
  or validation fails

Validation gate:

- release-input cannot be produced without a valid metadata-bundle archive
- release-input metadata matches archive contents
- metadata version `X.Y.Z.0` matches product/software version `X.Y.Z`

### Phase 5: Persistence And Metadata Bundle APIs

Deliverables:

- persist active metadata bundle and previous rollback candidate
- add metadata-bundle status, validate, install, and rollback APIs
- audit every install and rollback
- expose active metadata in setup-state
- persist active metadata-bundle digest and top-level directory name
- retain at least one previous compatible metadata bundle for rollback

Validation gate:

- invalid signature/schema/version cannot install
- failed install leaves active metadata unchanged
- rollback restores a previous compatible metadata bundle
- metadata-bundle install does not implicitly activate a new profile

### Phase 6: Profile Catalog Refactor

Deliverables:

- derive profile catalog from active metadata bundle
- keep profile list/get/validate/activate APIs
- remove profile create/update/delete handlers and tests
- remove custom profile UI create/edit/delete workflows
- update permissions from `profiles.manage` to `metadata.manage` and
  `profiles.activate`
- use `capabilities/catalog.yaml` for dependency, conflict, artifact, and
  license entitlement rules

Validation gate:

- profiles list reflects active metadata bundle
- metadata-bundle update can add a new profile without a software rebuild
- administrator cannot create or edit profiles through API/UI
- profile validation output names the active metadata version used

### Phase 7: License And Artifact Validation Integration

Deliverables:

- validate profile entitlement against current license/base-free state
- validate required artifacts against signed software-bundle manifest
- return grouped validation results for UI display
- prevent activation while licensing is unresolved
- treat license entitlement requirements as capability-local policy
- do not add a separate top-level entitlement-policy engine in v1

Validation gate:

- unresolved license blocks activation
- base/free entitlement allows only base/free capabilities
- valid profile plus missing artifact cannot activate
- valid profile plus missing entitlement cannot activate
- capability-local entitlement rules drive activation decisions

### Phase 8: UI Metadata Bundle And Profile Activation UX

Deliverables:

- add Admin / Metadata Bundle page
- show running software version and active metadata version
- support metadata-bundle upload validation and install confirmation
- support rollback when previous metadata exists
- update Admin / Profiles to read-only profile catalog plus validation and
  activation
- show capability-local requirements on profile detail pages: dependencies,
  required artifacts, and license entitlement
- remove any UI affordance for editing capability/license/artifact policy

Validation gate:

- first login licensing alert still behaves correctly
- metadata-bundle install flow is opaque/top-layer safe in desktop and mobile UI
- profile activation requires explicit confirmation
- no create/edit/delete profile UI remains

### Phase 9: Appliance-Release Final Bundle Assembly

Deliverables:

- `appliance-code` release-input contains a directory-based metadata bundle
  packaged as
  `appliance-metadata-bundle-X.Y.Z.0.tar.zst`
- `appliance-release` includes and verifies the base metadata bundle as a
  separate final-bundle artifact
- final release manifest records metadata-bundle path, digest, metadata version,
  compatible software version, and top-level directory name
- final bundle verification checks archive digest and metadata-bundle fields
- product-bundle sample and CI configs include the metadata-bundle artifact

Validation gate:

- final appliance bundle contains a separate verified metadata-bundle directory
  archive
- missing metadata-bundle artifact fails bundle assembly
- mismatched metadata-bundle metadata fails bundle verification

### Phase 10: Appliance-Ctl Install, State, And Diagnostics

Deliverables:

- `appliance-ctl` default-profile behavior verified
- installer extracts or stages the verified metadata-bundle directory
- installer validates selected profile against the base metadata bundle before
  install-time profile use
- installed-state records active metadata version, digest, and directory name
- `status`, `verify`, backup, restore, diagnostics, and support-bundle flows
  include active metadata
- install summary tells the operator to complete licensing after first login

Validation gate:

- fresh install with no profile uses `core`
- fresh install rejects a profile absent from the verified metadata bundle
- install does not ask for license
- installed state includes active metadata-bundle metadata

### Phase 11: End-To-End Release Validation

Deliverables:

- release verification covers metadata-bundle install, version rejection,
  rollback, licensing gate, profile catalog refresh, and activation validation
  failure modes
- upgrade flow installs the matching new base metadata bundle for the new
  software version
- metadata-only update flow is tested for same-software-version bundles

Validation gate:

- fresh install with no profile uses `core`
- fresh install does not ask for license
- first UI login shows licensing alert
- alert clears after base/free acceptance
- final appliance bundle contains a separate verified metadata-bundle directory
  archive
- software `X.Y.Z` rejects metadata bundles outside `X.Y.Z.N`
- metadata-bundle update can add a profile using already-installed artifacts
- profile activation cannot bypass policy, bundle, or license checks
- full software upgrade moves from `X.Y.Z.N` to new base metadata
  `A.B.C.0`

## Locked Decisions

- Default install profile is `core`.
- License setup is post-install only.
- Unresolved licensing alert appears after login until resolved.
- Accepting base/free entitlement resolves licensing.
- Profiles are vendor-defined through signed appliance metadata bundles.
- Administrators do not create/edit/delete profile definitions in v1.
- Metadata bundle version is `softwareVersion.policyRevision`, for example
  software `4.3.2` uses metadata `4.3.2.0`, `4.3.2.1`, and `4.3.2.2`.
- Metadata bundles are compatible only with the exact running software version
  matching the first three version segments.
- Policy-bundle updates cannot add missing software artifacts or runtime code.

## Open Decisions

- Exact metadata-bundle signing format and trust-root packaging.
- Whether metadata-bundle install requires resolved licensing or only
  administrator authorization. Profile activation still requires resolved
  licensing either way.
- How many previous metadata bundles to retain beyond one rollback candidate.
- Whether metadata-bundle install should require control-plane restart in the
  first implementation or can safely reload in process.
- Whether existing development custom-profile data should be migrated,
  ignored, or explicitly cleaned up when switching to metadata-bundle profiles.

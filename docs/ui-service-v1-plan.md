# UI Service V1 Plan

## Goal

Add a separate appliance UI service under `services/ui` and deploy it as its
own container, Kubernetes Deployment, Service, and public route in the complete
V1 appliance.

The UI service is intentionally separate from the control plane. The control
plane remains the owner of API behavior, persistence, authentication,
authorization, appliance capabilities, and operational workflows. The UI
service is a small server-driven web layer that renders HTML and calls the
control-plane APIs.

This does not create a second appliance bundle variant. The complete offline
bundle continues to ship the control plane, UI service, zot, Argo Workflows,
and all required images and charts as one product package.

## Product Direction

The UI should be boring in the best appliance sense: fast to load, easy to
inspect, simple to maintain, and usable without a JavaScript build system.

V1 UI technology choices:

- Go standard library HTTP server.
- Server-rendered HTML using Go's native `html/template`.
- HTMX for declarative interaction via `hx-get`, `hx-post`, `hx-target`, and
  `hx-swap`.
- Pico.css as the default class-light stylesheet for clean forms, tables, and
  layout without a custom design system.
- HTMX and CSS are served as local static assets from the UI service in the
  appliance bundle. Development can reference upstream versions, but installed
  appliance operation must not depend on a public CDN.
- No Node.js, npm, Webpack, Vite, React, SPA runtime, client-side state store,
  or custom browser fetch layer.
- No JSON returned for UI rendering routes. UI routes return full HTML pages or
  partial HTML fragments.

The browser talks to the UI service. The UI service talks to the control-plane
API over the in-cluster service DNS name.

See [UI To Control-Plane Route Mapping](ui-control-plane-route-mapping.md) for
the maintained operator-facing route map and the required documentation update
rule for UI/API integration changes.

## V1 Implementation Choices

Keep the V1 UI deliberately small and boring:

- Use only Go standard library packages for the HTTP server, templates, static
  file serving, cookies, and tests.
- Use `//go:embed` for templates and static assets so the UI container is a
  single self-contained binary plus minimal runtime files.
- Use `html/template` files with simple page and partial templates. Avoid
  `a-h/templ` in V1 because its code-generation step adds another toolchain and
  review surface without enough payoff for the initial appliance UI.
- Vendor pinned, minified `htmx.min.js` and `pico.min.css` under
  `services/ui/static/vendor/`. Do not fetch them during install or runtime.
- Add at most one small `app.css` for product spacing, header layout, and status
  badges. Avoid a custom component framework.
- Keep HTMX usage to form posts, panel refreshes, and small fragment swaps. Do
  not add custom JavaScript, browser-side state, JSON fetches, Alpine.js,
  hyperscript, Stimulus, Turbo, or similar libraries.
- Prefer normal full-page redirects after login/logout/setup. Use HTMX only
  where a partial update makes the page simpler for the operator.

## Non-Goals

- Do not move control-plane APIs, auth, RBAC, or business logic into the UI
  service.
- Do not introduce a frontend build pipeline.
- Do not introduce a separate UI product bundle or profile-specific bundle.
- Do not expose Argo Server or Argo UI.
- Do not make HTMX endpoints a public machine API. Machine clients should use
  the control-plane API and SDK.
- Do not publish a separate OpenAPI contract for HTMX fragment routes unless
  another service needs to call them. The durable machine contract remains the
  control-plane OpenAPI/API surface.

## Service Shape

Proposed location:

```text
services/ui/
  cmd/appliance-ui/main.go
  internal/config/
  internal/controlplane/
  internal/session/
  internal/ui/
  templates/
    layout.html
    pages/
    partials/
  static/
    app.css
    vendor/
      htmx.min.js
      pico.min.css
  Containerfile
  Makefile
  go.mod
```

The service should expose:

- `GET /health/live`
- `GET /health/ready`
- `GET /`
- `GET /login`
- `POST /login`
- `POST /logout`
- `GET /dashboard`
- `GET /partials/status`
- `GET /partials/session`

If browser-based first-admin creation is accepted for V1, add:

- `GET /setup`
- `POST /setup`

The root route should serve the base HTML shell with:

- HTMX script tag pointing at a bundled local static file.
- Pico.css stylesheet pointing at a bundled local static file.
- a stable main content container.
- initial page content chosen from setup, login, or dashboard state.

Partial routes should return only HTML fragments, for example:

```html
<section id="status-panel">
  ...
</section>
```

Those fragments are swapped into existing DOM targets by HTMX attributes.

## Authentication Model

The UI service should not validate passwords itself. It should call:

- `POST /api/v1/auth/login`
- `POST /api/v1/auth/logout`
- `GET /api/v1/auth/session`

The UI service should store browser session state in secure, HTTP-only cookies.
The cookie should hold only UI session material needed to call the control
plane, not durable product state.

Recommended first implementation:

- Store an opaque UI session ID in an HTTP-only, secure, SameSite cookie.
- Keep control-plane access and refresh credentials in the UI service's
  server-side session store.
- Attach the access token as `Authorization: Bearer ...` when the UI service
  calls control-plane APIs.
- On an expired access token, refresh server-side and retry once.
- Clear cookies on logout.

For V1, the session store can be in memory and the UI Deployment should run a
single replica. If UI replicas are increased later, replace the in-memory store
with a shared session backend or use sticky routing. The important boundary is
that browser code never manages tokens directly.

## First-Admin Setup Decision

There is one product decision to settle before implementation.

Today the first administrator bootstrap is node-local. `zonctl` runs a
control-plane container command through Kubernetes after the chart is ready.
That is intentionally not exposed as public HTTP.

The requested UI flow wants browser-based first-admin creation. These can fit
together, but only if we add a narrowly scoped remote bootstrap contract.

Recommended V1 approach:

- Installer generates a one-time bootstrap secret on the target.
- The secret is mounted into the control-plane and UI pods through Kubernetes
  Secret references.
- UI setup is enabled only when the control plane reports that no user exists.
- `POST /setup` must include or possess the one-time bootstrap material.
- Control plane consumes the bootstrap material transactionally when creating
  the first admin.
- After success, setup routes return a non-sensitive "already initialized"
  state.

Conservative alternative:

- Keep bootstrap node-local only.
- UI starts at login.
- If no admin exists, UI shows a setup-needed page instructing the operator to
  complete installer bootstrap.

Recommendation: implement the remote bootstrap contract, but keep it
single-use, short-lived, and unavailable after initialization. This preserves
the appliance-friendly first-run experience without turning bootstrap into a
normal public API.

## Initial UI Scope

Milestone 1 should prove integration, not build a large console.

Pages and fragments:

- Login page: username and password form.
- Dashboard page: show current user, appliance version, appliance profile, and
  a compact health summary.
- Status fragment: refreshes via `hx-get="/partials/status"` into a dashboard
  panel.
- Session fragment: displays username or user ID and auth method.

Browser-based first-admin bootstrap is deferred from this milestone until the
remote bootstrap contract is accepted. Milestone 1 starts at login and keeps
the existing installer or node-local bootstrap flow as the authority for first
administrator creation.

Suggested dashboard data:

- `GET /api/v1/auth/session`
- `GET /version` through an internal or public control-plane route, depending
  on the final route policy.
- `GET /api/v1/users` only if the logged-in user has permission.
- A minimal health endpoint suitable for UI display. If the existing health
  route remains internal-only, add a small public status endpoint guarded by
  auth.

## Helm Chart Changes

Keep one appliance chart, but add UI resources to it.

Add chart values:

```yaml
ui:
  enabled: true
  image:
    repository: appliance-ui
    tag: ""
    digest: ""
    pullPolicy: IfNotPresent
  service:
    port: 8080
  config:
    controlPlaneBaseURL: ""
    controlPlaneInternalBaseURL: ""
```

When those UI control-plane URL fields are empty, the chart should derive them
from the rendered control-plane Service names. Explicit values remain available
only for advanced overrides.

Add templates:

- UI Deployment.
- UI Service.
- UI ConfigMap.
- UI NetworkPolicy.
- IngressRoute rule for browser UI paths.

Routing intent:

- `/api/v1/*` goes to control plane.
- `/mcp` goes to control plane.
- `/` and UI-owned paths go to UI service.

This UI slice only changes the browser-UI versus control-plane split. Any
future `/v2/*` OCI registry routing remains the responsibility of the registry
service and its owning chart or workstream rather than this milestone.

The chart should continue to support disabled capabilities by relying on the
control plane to return `404` for disabled capability APIs. The UI should avoid
showing capability-specific panels when the control plane reports those APIs as
unavailable.

## Release Input And Bundle Changes

`appliance-code` must produce a UI image archive alongside the control-plane
image archive.

`appliance-ctl` must understand that release-input includes another OCI image
artifact and must carry it into the final signed bundle as an `oci-images`
entry.

Required changes:

- Extend `scripts/package/archive-release-input.sh` to accept:
  - `--ui-image PATH`
  - `--ui-image-reference REF`
- Extend `release-input.v1` schema with `artifacts.uiImage`.
- Extend `appliance-ctl/internal/releaseinput` to parse and verify
  `uiImage`.
- Extend `appliance-release/scripts/package/init-simple-workspace.sh` to add
  the UI image archive to bundle entries.
- Extend `appliance-release/scripts/ci/build-full-bundle.sh` to export the UI
  image archive from `appliance-code` and pass it into release-input creation.
- Ensure final release manifests contain both images as `oci-images`:
  - control-plane image
  - UI image

The existing install and upgrade logic should then preload the UI image through
the same `oci-images` path used for the control-plane image.

## Repository Work Plan

### appliance-code

1. Add `services/ui` Go module, server, templates, static assets, tests,
   Containerfile, and Makefile.
2. Add UI image build/export target to the dev-container packaging flow.
3. Extend `archive-release-input.sh` and release-input schema for `uiImage`.
4. Extend the appliance chart with UI Deployment, Service, ConfigMap,
   NetworkPolicy, and IngressRoute rules.
5. Update topology docs to include the UI pod as an always-running product pod.
6. Add chart policy tests for UI resources and ingress routing.

### appliance-ctl

1. Extend release-input structs and schema validation for `uiImage`.
2. Verify UI image digest and size during release-input load.
3. Ensure bundle assembly carries UI image into `oci-images`.
4. Add tests showing both control-plane and UI images are imported on install
   and upgrade.

### appliance-release

1. Update full-bundle build script to collect the UI image archive from
   `appliance-code`.
2. Update simple workspace generation to include the UI image in signed bundle
   entries.
3. Add target verification for UI reachability:
   - `GET /` returns HTML.
   - login page or dashboard is visible depending on initialization state.
4. Update release and target runbooks with the browser URL and expected UI
   behavior.

## Validation Plan

Local validation:

- `go test ./...` in `services/ui`.
- chart render tests prove UI Deployment, Service, ConfigMap, NetworkPolicy,
  and IngressRoute are present.
- UI handler tests assert full routes return complete HTML and partial routes
  return HTML fragments.
- No UI route returns JSON.

Bundle validation:

- release-input contains both `controlPlaneImage` and `uiImage`.
- final release manifest includes both OCI image archives.
- install preloads both images.
- chart deploys both pods.

Target validation:

- `kubectl get pods -n appliance-system` shows control-plane and UI pods running.
- `curl -k https://<appliance>/` returns HTML.
- `curl -k https://<appliance>/api/v1/auth/session` remains routed to the
  control plane and returns the expected auth response.
- Browser can open the login page or setup page.
- After login, dashboard renders current session and basic appliance status.

## Open Questions

- Should V1 allow browser-based first-admin bootstrap using a one-time secret,
  or keep first-admin creation strictly node-local?
- Should the control plane expose a small authenticated UI-status endpoint, or
  should the UI compose status from existing APIs only?
- Should UI session storage start with secure token cookies for speed, or use
  an opaque UI session ID from day one?
- Should the UI service be enabled for all appliance profiles, including
  `storage`, or only when the `base` capability is enabled? The recommended
  answer is all profiles, because UI belongs to `base`.

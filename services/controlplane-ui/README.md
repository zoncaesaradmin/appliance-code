# Appliance Control-Plane UI

This service owns the browser UI for the appliance control plane. It is a
React + TypeScript single-page application served by a small Go static host.
The browser calls the control-plane OpenAPI endpoints directly under
`/api/v1`; the UI service no longer owns HTMX handlers or browser-facing UI
API routes.

## Runtime Modes

### Packaged Appliance Runtime

The container image builds the SPA with Vite, copies the compiled assets to
`/opt/appliance-ui/dist`, and starts the Go host with:

```bash
APPLIANCE_UI_STATIC_DIR=/opt/appliance-ui/dist
```

The Go host serves:

- `/` and deep SPA routes such as `/admin/lan-services`
- immutable files under `/assets/`
- `/health/live`
- `/health/ready`
- `/version` (proxies the control-plane product version JSON; Traefik
  routes this path to the UI catch-all rather than `/api/v1`)

Readiness still checks the configured internal control-plane URL so the UI pod
does not report ready when the appliance API is unavailable.

### Local Mock Development

Mock mode runs entirely on a developer workstation and does not require a live
appliance:

```bash
npm ci
npm run dev:mock
```

Mock mode uses an in-browser control-plane implementation. It is intended for
layout work, interaction design, and fast local development only.

### Local Development Against A Real Appliance

Use the Vite development server with a proxy target:

```bash
npm ci
VITE_CONTROL_PLANE_PROXY_TARGET=https://appliance.example.internal npm run dev
```

The SPA still calls `/api/v1`, `/version`, and `/health/ready` from the
browser. Vite proxies those paths to the configured appliance while preserving
the same browser-side API shape used in production.

If the appliance API is already reachable from the same origin, omit
`VITE_CONTROL_PLANE_PROXY_TARGET`.

## Build And Verification

```bash
make ui-build
make ui-test
make ui-openapi-check
make lint
make test
make verify
```

`make build` runs the frontend build first and then compiles the Go static host.
`make lint` runs Go vet/gofmt checks and TypeScript typechecking.
`make ui-openapi-check` verifies the generated TypeScript OpenAPI types are in
sync with `../../docs/openapi/control-plane-v1.yaml`. `make ui-test` runs the
Vitest/jsdom browser-side smoke suite for auth storage, direct API client
behavior, mock control-plane behavior, and navigation rules. The repo-level
`make verify` includes this service through the top-level module loop.

Regenerate the OpenAPI types after changing the control-plane spec:

```bash
npm run openapi:types
```

## Image Build Inputs

### Build And Push Only The UI Image

On the Linux build server, use the shared dev container and the service image
target. When `DEV_REGISTRY` points at the artifact registry,
`make -C services/controlplane-ui image` automatically builds, logs in, tags,
and pushes the UI image. You do not need to set `SERVICE_IMAGE_*` variables
unless you want to override the default destination.

```bash
cd /path/to/appliance-code

export DEV_REGISTRY=artifact-dns-1.appliance.internal
export DEV_IMAGE_REPO=development-container
export DEV_IMAGE_NAME=dev-build
export DEV_IMAGE_TAG=latest
export DEV_REGISTRY_USER=<artifact-registry-username>
export DEV_REGISTRY_TOKEN=<artifact-registry-token>
export DEV_REGISTRY_TLS_VERIFY=false

make dev-shell

# Inside the dev container:
make -C services/controlplane-ui image
```

By default, the image target pushes:

```text
${DEV_REGISTRY}/appliance-images/appliance-ui:<git-describe>
${DEV_REGISTRY}/appliance-images/appliance-ui:latest
```

Use explicit service overrides only when the service image must go somewhere
different from `${DEV_REGISTRY}/appliance-images/appliance-ui` or needs a
custom tag:

```bash
make -C services/controlplane-ui image \
  SERVICE_IMAGE_REGISTRY=<other-registry-host> \
  SERVICE_IMAGE_REPO=appliance-images \
  SERVICE_IMAGE_TAG=ui-test-$(git rev-parse --short HEAD)
```

For a local-only image with no push, use:

```bash
make -C services/controlplane-ui image-local
```

### Base Image Overrides

The UI `Containerfile` accepts build-stage base images as build arguments so
CI/release jobs can provide mirrored or digest-pinned references:

```bash
make image-local \
  UI_NODE_IMAGE=registry.internal/node@sha256:<digest> \
  UI_GO_IMAGE=registry.internal/golang@sha256:<digest> \
  UI_RUNTIME_IMAGE=registry.internal/alpine@sha256:<digest>
```

The release-input helper forwards the same values from `UI_NODE_IMAGE`,
`UI_GO_IMAGE`, and `UI_RUNTIME_IMAGE`, or from its explicit `--node-image`,
`--go-image`, and `--runtime-image` options.

## Layout Rules

- Home is the default landing mode and opens directly to the dashboard page.
- Manage, Analyze, and Admin use the left icon rail plus a transient feature
  selector.
- Each feature page uses simple top tabs for views within that feature.
- The header owns product identity, user actions, future notifications, and
  help/about entry points.
- Placeholder cards are allowed only when the backend API does not exist yet;
  they should name the missing appliance capability instead of pretending the
  workflow is complete.

## Current Backend-Dependent Placeholders

These areas are intentionally scaffolded for the new layout but need backend
API work before they can become fully functional:

- appliance-wide notifications and alarms
- detailed admin system service/metric status
- help/about/version-detail content beyond the existing `/version` API
- richer workflow analytics
- profiles and licensing administration

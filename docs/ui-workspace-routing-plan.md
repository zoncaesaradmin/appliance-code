# Unified UI workspace and login plan

## Decision

The appliance has one public origin and one authentication entry point, with
two intentionally separate browser workspaces:

```text
/                 Zonity application entry
/app/...          application PWA routes
/console/...      appliance operations console
/login?next=...   shared login surface
/setup            first administrator setup
```

`/console` is the operator namespace. It is deliberately broader than
`/admin`: the existing appliance surface includes building, files, workflows,
AI, host services, and administration.

## Login behavior

The public application entry has one primary action. It does not present an
operator/application chooser. An operator who opens `/console` directly gets
the same login component with console context and is returned to the console
after authentication.

The browser carries a validated internal `next` path through authentication.
The server remains the authority for permissions; the route is only a
navigation intent. A signed-in principal with both application and operator
permissions may switch workspaces from the account menu. A principal with one
workspace is sent directly there.

First-admin setup remains `/setup` and remains an operator operation.

## Compatibility policy

Existing control-plane URLs remain valid during migration:

```text
/home/*       existing appliance home
/manage/*     existing builder/files/media surface
/analyze/*    existing workflow surface
/admin/*      existing administration surface
```

The new `/console` entry initially aliases the existing appliance shell. The
legacy paths are not removed or renamed until the application UI artifact and
its API services are present in the signed app-stack delivery package.

## API and deployment boundary

UI route changes do not rename API contracts. Existing control-plane APIs stay
under `/api/v1`, ordering stays under `/api/ordering/v1`, and the channel
callback remains `/webhook`.

The first application PWA slice is now delivered by the control-plane UI image:
it provides an installable static shell, `/app/login`, and the protected `/app`
entry while reusing the existing control-plane authentication contract. Its
Shared/Core and Rentals API contracts still must be delivered and routed; the
initial application shell currently contains placeholders for those surfaces.
The current appliance bundle contains the ordering and channel-integration
services, but does not yet contain every PWA backend service from `capp`.

## Implementation order

1. Make login context-aware and preserve the requested internal path.
2. Add `/console` aliases without changing existing routes or APIs.
3. Add application business screens and their signed backend contracts inside
   the now-live `/app` PWA shell.
4. Add role-aware workspace switching after authentication.
5. Keep `/` as the Zonity application entry; operators use `/console` or
   `/login` explicitly while application API surfaces are added incrementally.

## Verification gates

- Existing control-plane UI navigation and API calls remain unchanged.
- `/console` redirects unauthenticated users through `/login` and returns them
  to the console after login.
- Open redirects are rejected; `next` accepts only internal appliance paths.
- Existing `/home`, `/manage`, `/analyze`, and `/admin` bookmarks still render.
- The signed bundle remains portable; no hostname, IP, or cloud-specific URL is
  embedded in the UI image.

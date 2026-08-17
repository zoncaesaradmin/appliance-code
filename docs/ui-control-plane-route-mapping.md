# UI To Control-Plane Route Mapping

This document is the operator-facing map between browser-visible UI routes and
the downstream control-plane API calls the UI service makes on the browser's
behalf.

The key architectural rule is:

- the browser talks to the UI service
- the UI service talks to the control-plane API
- machine clients should use the control-plane API directly

This means browser devtools will often show only a UI route such as
`POST /builder/workspaces`, while the actual appliance business action is a
separate server-side call from the UI service to the control plane.

## Runtime Tracing

These UI-to-control-plane traces are enabled by default.

To disable them temporarily:

```bash
APPLIANCE_UI_CONTROL_PLANE_TRACE=false
```

The UI service logs one structured event per downstream control-plane call,
including:

- downstream HTTP method
- downstream HTTP path
- expected status
- received status
- duration
- redacted request summary
- redacted response summary
- trace ID for end-to-end correlation

These trace events are written by the UI service itself, so operators should
look in the UI service logs first:

- `/data/zon/logs/ui/application.log`
- `kubectl logs deploy/ui-server -n ace-apps`
- `/data/zon/logs/ui/stdout.log`

The control plane writes its own redacted API exchange logs too. For the same
browser action, operators can also inspect:

- `/data/zon/logs/api-server/application.log`
- `kubectl logs deploy/controlplane -n ace-apps`
- `/data/zon/logs/api-server/stdout.log`

Useful event names:

- UI service: `control plane API call`
- control plane: `http api exchange`
- control plane: `http request`

## Current Mapping

| Browser-visible route | UI handler | Downstream control-plane call(s) | Browser success behavior |
| --- | --- | --- | --- |
| `GET /health/ready` | `ready` | `GET /health/ready` on the control-plane internal listener | `200 ready` plain text |
| `POST /login` | `login` | `POST /api/v1/auth/login` | `303` redirect to `/dashboard` |
| `POST /setup` | `setup` | `POST /api/v1/setup/first-admin`, then `POST /api/v1/auth/login` | `303` redirect to `/dashboard` |
| `POST /logout` | `logout` | `POST /api/v1/auth/logout` | `303` redirect to `/login` |
| `GET /home` | React `HomePage` Overview | `GET /version`, `GET /health/ready`, `GET /api/v1/appliance/identity`, `GET /api/v1/appliance/setup-state` | SPA page |
| `GET /home/connectivity` | React `HomePage` Connectivity | Same overview fetches as `/home` | SPA page |
| `GET /home/audit-logs` | React `HomePage` Audit Logs | Session must include `audit.read`; `GET /api/v1/audit/events?limit=10` with optional `cursor` for Next page | SPA page |
| `GET /account/api-keys` | React `AccountPage` API Keys | `GET /api/v1/tokens`; create uses `POST /api/v1/tokens`; revoke uses `DELETE /api/v1/tokens/{id}` | SPA page; create shows the raw secret once; list shows active (non-revoked) tokens only |
| `GET /manage/artifacts` | React `ArtifactsPage` Catalog | `GET /api/v1/registry/repositories`; `GET /api/v1/registry/repositories/{repository}/tags`; optional referrers lookup | SPA page with link to Account → API Keys for registry client credentials |
| `GET /manage/artifacts/grants` | React `ArtifactsPage` Grants | `GET /api/v1/registry/grants`; create `POST /api/v1/registry/grants`; delete `DELETE /api/v1/registry/grants/{id}` | SPA page |
| `GET /partials/status` | `dashboardData` | Same downstream calls as `GET /dashboard` | `200` HTML partial |
| `GET /partials/session` | `dashboardData` | Same downstream calls as `GET /dashboard` | `200` HTML partial |
| `GET /manage/builder` (legacy alias `/manage/builder/builds`) | React `BuilderPage` Build | Loads current workspace, build-targets, and `GET /api/v1/jobs` (build-type rows); row click opens details via `GET /api/v1/jobs/{jobId}` + steps; submit opens a dialog that `POST`s `/api/v1/current-workspace/builds` | SPA page |
| `GET /manage/builder/workspaces` | React `BuilderPage` Workspaces | Loads work-profiles, workspaces, current-workspace | SPA page |
| `GET /manage/builder/settings` (legacy alias `/manage/builder/git-access`) | React `BuilderPage` Settings | Loads catalog + git-access only; mutations refresh those two APIs | SPA page |
| `GET /manage/files` | React `FilesPage` | `GET /api/v1/files` (and `GET /api/v1/files/{path}` when browsing a directory); upload dialog `POST`s octet-stream to `/api/v1/files/{logical-path}`; row ⋮ Delete uses `DELETE /api/v1/files/{path}` | SPA page |
| `POST /builder/git-access` | `configureBuilderGitAccess` (legacy UI service) | Session check/refresh as needed; `PUT /api/v1/builder/git-access/{name}` | `303` redirect to `/builder/workspaces` |
| `POST /builder/builds` | `submitBuilderBuild` | Session check/refresh as needed; `POST /api/v1/current-workspace/builds` with `targetName` and optional `imageTag` | `303` redirect to `/builder/workspaces` on success; re-renders the builder page with an error for missing catalog/Git access (`412`), workspace not ready (`409`), unknown target / no workspace (`404`), or other validation failures |
| `POST /builder/workspaces` with `selected_workspace_id=<existing>` | `createBuilderWorkspace` | Session check/refresh as needed; `POST /api/v1/current-workspace` | `303` redirect to `/builder/workspaces` |
| `POST /builder/workspaces` with `selected_workspace_id=new` or no selection | `createBuilderWorkspace` | Session check/refresh as needed; `GET /api/v1/workspaces`; then either `POST /api/v1/current-workspace` for an existing same-name/same-profile workspace, or `POST /api/v1/workspaces` to create a new one; if the catalog or required Git credentials are missing, the control plane returns `412` and the UI re-renders the page with an error instead of redirecting | `303` redirect to `/builder/workspaces` on success |
| `POST /builder/current-workspace` | `setBuilderCurrentWorkspace` | Session check/refresh as needed; `POST /api/v1/current-workspace` | `303` redirect to `/builder/workspaces` |
| `POST /builder/workspaces/delete` | `deleteBuilderWorkspace` | Session check/refresh as needed; `DELETE /api/v1/workspaces/{workspaceId}` | `303` redirect to `/builder/workspaces?workspace_id=new` |
| `GET /partials/builder/work-profile` | `builderWorkProfilePartial` | Session check/refresh as needed; `GET /api/v1/work-profiles` | `200` HTML partial |

## Workspace list UX

The Builder **Workspaces** tab (`/manage/builder/workspaces`) mirrors the
Settings Git-credentials card:

- Card heading, then the workspace list (or an empty-state message when none
  exist). Current workspace is marked on the matching row as `(current)`.
- One row per workspace (name + profile + status from the API); row click opens
  a details dialog
- Trailing ⋮ menu: View details / Set current / Delete
- `+ Create workspace` opens a dialog (not an inline form); success closes the
  dialog and refreshes the list

## Build list UX

The Builder **Build** tab (`/manage/builder`) uses the same card pattern:

- Card heading, then the build list (or an empty-state message when none exist)
- One row per build job (submission ID, target, status, submitted time,
  completed time); clicking a row opens a details dialog that loads
  `GET /api/v1/jobs/{jobId}` and `GET /api/v1/jobs/{jobId}/steps`
- Trailing ⋮ menu: View details / Cancel (when running)
- `+ Submit build` opens a dialog for target + image tag against the current
  workspace; success closes the dialog and refreshes `GET /api/v1/jobs`

## Settings list UX

Builder **Settings** (`/manage/builder/settings`) uses the same list pattern:

- Catalog: empty-state when unset; one row when configured; click opens the
  full document; ⋮ has View details / Download YAML / Replace catalog;
  `+ Add catalog` opens upload
- Git credentials: empty-state when none; one row per credential; click opens
  details; ⋮ has View details / Edit / Delete; `+ Add credential` opens the
  create dialog

## Workspace Provisioning Flow

`POST /api/v1/workspaces` is now asynchronous.

The control-plane API creates the workspace row immediately, sets it as the
current workspace, creates a `workspace_prepare` job, submits the workflow,
and returns the first known workspace state in the response body.

Expected workspace state progression:

1. `pending`
2. `ready` after the workspace workflow finishes successfully
3. `failed` if workflow submission or workspace materialization fails

The UI route still returns `303` to the browser on success because the browser
interaction is a post-redirect-get flow. The control-plane response body for the
workspace create call is visible in the UI `control plane API call` log entry.

The control plane also writes durable functional lifecycle events for this path
to `/data/zon/logs/api-server/application.log`:

- `workspace provisioning workflow submitted`
- `workspace provisioning workflow state changed`
- `workspace status reconciled`
- `workspace provisioning workflow submission failed`
- `workspace provisioning workflow missing`

## Builder Settings Flow

Builder workspace and build flows depend on a single runtime build catalog plus
named appliance-side HTTPS Git credentials (one per Git host).

- Browser users configure both through the Builder **Settings** tab
  (`/manage/builder/settings`).
- Catalog upload opens an upload dialog that calls `PUT /api/v1/builder/catalog`
  (YAML or JSON). Clicking the configured catalog row opens a details dialog
  with the full document; download uses the `document` field from
  `GET /api/v1/builder/catalog`.
- Credential rows are clickable for details; saves open an add/edit dialog that
  calls `PUT /api/v1/builder/git-access/{name}`; deletes use
  `DELETE /api/v1/builder/git-access/{name}` from the row ⋮ menu. The form
  itself stays dialog-only.
- The control plane stores the catalog in SQLite and each credential as a
  Kubernetes Secret named `git-access-<name>` in `appliance-builds`.
- Workspace creation returns `412 Precondition Failed` until a catalog is
  configured and every catalog Git host has a matching credential.

For operators, the practical sequence is:

1. Install the builder profile (catalog starts blank).
2. Create a Git provider personal access token outside the appliance.
3. Sign in to the appliance UI as an administrator.
4. Open Builder **Settings**.
5. Upload an appliance-native `build-catalog.yaml` (see the in-repo example).
6. Save `credential name + git server + git username + personal access token`
   for each required server.
7. Create the first workspace only after catalog status is configured and Git
   coverage is complete.

## Operator Debugging Notes

### Example: `POST /builder/workspaces`

When the browser shows:

- `POST /builder/workspaces`
- response code `303`

that means the browser only observed the UI route. On the successful path, the
UI handler has already completed one of these server-side control-plane actions:

1. `POST /api/v1/current-workspace` to switch to an existing workspace
2. `POST /api/v1/workspaces` to create a new workspace and start provisioning

If the downstream control-plane call fails, the UI handler does not return
`303`. It renders the builder page with an error message instead.

So when browser devtools only show `303`, that is the UI service's response to
the browser, not the control-plane API response. The control-plane response is
visible in the UI service `control plane API call` log entry and in the control
plane `http api exchange` log entry. Use the shared trace ID to follow the same
browser action across both services.

### Where To Look First

To answer "did the request reach the control plane API or stop in the UI
service?":

1. Check the browser-visible UI route and response code.
2. Check the UI service trace log event for the downstream control-plane call.
3. Check the control-plane `http api exchange` log entry for the matching trace
   ID and request path.
4. If needed, check durable state such as the `workspaces`, `current_workspaces`,
   and `jobs` tables for the current workspace and its provisioning job.

### Following A Pending Workspace

If `POST /api/v1/workspaces` returns a workspace with `status: "pending"`, the
next places to inspect are:

1. The matching control-plane `workspace provisioning workflow submitted` log
   entry. It includes the `jobID`, `workflowName`, `workspaceName`, and trace ID.
2. `GET /api/v1/jobs` and locate the `workspace_prepare` job for that
   workspace. Use the returned job ID with:
   - `GET /api/v1/jobs/{jobId}`
   - `GET /api/v1/jobs/{jobId}/steps`
   - `GET /api/v1/jobs/{jobId}/logs`
3. Workflow-engine runtime state in the build namespace:
   - `kubectl -n appliance-builds get workflows`
   - `kubectl -n appliance-builds get pods`
   - `kubectl -n appliance-builds logs <workspace-prepare-pod>`
4. Workspace storage state:
   - `kubectl -n appliance-builds get pvc,pv`
   - inspect `/data/zon/workspaces/<workspace-name>` on the host

If the workflow is still running, later `GET /api/v1/workspaces` or
`GET /api/v1/current-workspace` calls will trigger reconciliation and move the
workspace from `pending` to `ready` or `failed`.

## Browser SPA mapping (current controlplane-ui)

The SPA talks to the control-plane API **directly** (not via a Go UI reverse
proxy). Notable Admin host configuration routes:

| Browser route | UI surface | Downstream control-plane call(s) | Success behavior |
| --- | --- | --- | --- |
| `GET /admin/host-services` | `AdminHostServicesPage` Network tab | `GET /api/v1/appliance/identity`; `GET /api/v1/host/info`; `GET /api/v1/host/health`; `GET /api/v1/host/wifi`; `GET /api/v1/host/wifi-ap` | Host network (primary LAN IPv4 + Ethernet/Wi-Fi/Wi-Fi AP status and per-link addresses from host-agent) + client Wi-Fi card + Wi-Fi AP card; independent loads. It does not scan before the adapter is enabled. |
| Enable client Wi-Fi | `enableWifiAdapter` | `PUT /api/v1/host/wifi/enable` | Brings the adapter up, opens the network selector, and then starts the first scan. |
| Scan Wi-Fi networks | `scanWifiNetworks` | `GET /api/v1/host/wifi/scan` | Available only after adapter enablement; refreshes scanned SSIDs, security labels, signal levels, and concurrent-mode detail. |
| Connect or Disconnect client Wi-Fi | `applyClientWifi` | `PUT /api/v1/host/wifi` with `{desired,ssid,psk,security}` | Connects appliance client Wi-Fi to the selected SSID or disconnects it; updates card state and connection message |
| `GET /admin/host-services/mdns` | `AdminHostServicesPage` mDNS tab | `GET /api/v1/host/mdns` | mDNS status card, including advertised `hostname.local` when available |
| Enable or Disable mDNS | `applyHostMDNS` | `PUT /api/v1/host/mdns` with `{desired}` | One action button from status (`desired`/`actual`); busy label while apply runs; card refresh with advertised `hostname.local` |
| Enable Wi-Fi AP | `applyHostWifiAP` | `PUT /api/v1/host/wifi-ap` with `{desired:true,psk}` (PSK never logged) | Shown only when AP is off; single PSK field (no confirm) with show/hide toggle; busy “Enabling…”; opens as `https://manage.ap/` (fixed IP `https://10.42.0.1/`); soft reasons such as `packages_missing` |
| Restart Wi-Fi AP | `applyHostWifiAP` | `PUT /api/v1/host/wifi-ap` with `{desired:true}` (omit PSK; host-agentd reuses stored secret) | Shown when Desired is on but Actual is not active (typical after reboot before/without auto-reconcile); busy “Restarting…” |
| Disable Wi-Fi AP | `applyHostWifiAP` | `PUT /api/v1/host/wifi-ap` with `{desired:false}` | Shown only when AP is on; busy “Disabling…” then switches to enable control |
| `GET /admin/lan-services` | React `DNSPage` (Admin → LAN Services → DNS tab) | `GET /api/v1/dns/records` | List zone records; page title LAN Services |
| Add or update DNS record | `upsertDNSRecord` | `PUT /api/v1/dns/records/{name}` with `{ipv4,ttl}` | Refresh records list |
| Delete DNS record | `deleteDNSRecord` | `DELETE /api/v1/dns/records/{name}` | Refresh records list |
| `GET /manage/dns` (legacy) | SPA redirect | — | `replace` navigate to `/admin/lan-services` |
| `GET /admin/licensing` | `AdminLicensingPage` | `GET /api/v1/licensing/status` | Status + accept/import forms |
| Accept base entitlement | `acceptBase` + `withViewSync` | `POST /api/v1/licensing/base-entitlement/accept` | Local status update; button greys when `resolved`; `requestViewSync` for `shell.alerts` / `shell.bootstrap` / `page` (+ tags `licensing`, `setup`) so header alerts and home card clear without navigating away |
| Import offline license | `importLicense` + `withViewSync` | `PUT /api/v1/licensing/license` | Same view-sync plan as accept; status refresh |
| Header Alerts | `Shell` notifications menu | `GET /api/v1/notifications`; dismiss → `POST /api/v1/notifications/{id}/acknowledge` | Re-fetched on route change and whenever `shell.alerts` is invalidated via view-sync |

### View sync (generic post-mutation UI invalidation)

Mutations that change shared chrome or cross-page setup must not hard-code the
notification widget. After success they call `requestViewSync` /
`withViewSync` (`services/controlplane-ui/src/lib/viewSync.ts`) with a plan:

- **regions**: `shell.alerts` | `shell.bootstrap` | `page` | `app`
- **tags** (optional): domain topics such as `licensing`, `setup`, `profiles`

Subscribers:

- `Shell` listens to `shell.alerts`
- `App` listens to `shell.bootstrap` / `app`
- Pages opt in with `useViewSyncGeneration("page")` and/or `useViewSyncTag(...)`

Permissions: `host.read` for status, `host.write` for apply. Admin mode visibility
still requires a system-administrator session for the left-rail entry.

## Maintenance Rule

Whenever a UI route is added, removed, or changed, and whenever a UI handler
starts calling a different control-plane API route or method, update this
document in the same change.

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

- `/var/log/appliance/ui/application.log`
- `kubectl logs deploy/control-plane-ui -n appliance-system`
- `/var/log/appliance/ui/stdout.log`

The control plane writes its own redacted API exchange logs too. For the same
browser action, operators can also inspect:

- `/var/log/appliance/control-plane/application.log`
- `kubectl logs deploy/control-plane -n appliance-system`
- `/var/log/appliance/control-plane/stdout.log`

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
| `GET /dashboard` | `dashboardData` | `GET /api/v1/auth/session`; if expired, `POST /api/v1/auth/refresh` then `GET /api/v1/auth/session`; `GET /version` on the internal listener; `GET /health/ready` on the internal listener | `200` full HTML page |
| `GET /partials/status` | `dashboardData` | Same downstream calls as `GET /dashboard` | `200` HTML partial |
| `GET /partials/session` | `dashboardData` | Same downstream calls as `GET /dashboard` | `200` HTML partial |
| `GET /builder/workspaces` | `builderPageData` | Session check/refresh as needed; `GET /api/v1/work-profiles`; `GET /api/v1/builder/git-access`; `GET /api/v1/workspaces`; `GET /api/v1/current-workspace` | `200` full HTML page |
| `POST /builder/git-access` | `configureBuilderGitAccess` | Session check/refresh as needed; `PUT /api/v1/builder/git-access` | `303` redirect to `/builder/workspaces` |
| `POST /builder/workspaces` with `selected_workspace_id=<existing>` | `createBuilderWorkspace` | Session check/refresh as needed; `POST /api/v1/current-workspace` | `303` redirect to `/builder/workspaces` |
| `POST /builder/workspaces` with `selected_workspace_id=new` or no selection | `createBuilderWorkspace` | Session check/refresh as needed; `GET /api/v1/workspaces`; then either `POST /api/v1/current-workspace` for an existing same-name/same-profile workspace, or `POST /api/v1/workspaces` to create a new one; if the shared Git credential is still missing, the control plane returns `412` and the UI re-renders the page with an error instead of redirecting | `303` redirect to `/builder/workspaces` on success |
| `POST /builder/current-workspace` | `setBuilderCurrentWorkspace` | Session check/refresh as needed; `POST /api/v1/current-workspace` | `303` redirect to `/builder/workspaces` |
| `POST /builder/workspaces/delete` | `deleteBuilderWorkspace` | Session check/refresh as needed; `DELETE /api/v1/workspaces/{workspaceId}` | `303` redirect to `/builder/workspaces?workspace_id=new` |
| `GET /partials/builder/work-profile` | `builderWorkProfilePartial` | Session check/refresh as needed; `GET /api/v1/work-profiles` | `200` HTML partial |

## Workspace Provisioning Flow

`POST /api/v1/workspaces` is now asynchronous.

The control-plane API creates the workspace row immediately, sets it as the
current workspace, creates a `workspace_prepare` job, submits the Argo workflow,
and returns the first known workspace state in the response body.

Expected workspace state progression:

1. `pending`
2. `ready` after the workspace workflow finishes successfully
3. `failed` if workflow submission or workspace materialization fails

The UI route still returns `303` to the browser on success because the browser
interaction is a post-redirect-get flow. The control-plane response body for the
workspace create call is visible in the UI `control plane API call` log entry.

The control plane also writes durable functional lifecycle events for this path
to `/var/log/appliance/control-plane/application.log`:

- `workspace provisioning workflow submitted`
- `workspace provisioning workflow state changed`
- `workspace status reconciled`
- `workspace provisioning workflow submission failed`
- `workspace provisioning workflow missing`

## Builder Git Access Flow

Builder workspace and build flows now depend on one shared appliance-side HTTPS
Git credential.

- Browser users configure it through `POST /builder/git-access`.
- The UI service translates that into `PUT /api/v1/builder/git-access`.
- The control plane stores it in a Kubernetes Secret in `appliance-builds`.
- Workspace creation and direct build submission return `412 Precondition Failed`
  until that credential exists.

For operators, the practical sequence is:

1. Create a Git provider personal access token outside the appliance.
2. Sign in to the appliance UI as an administrator.
3. Open the Builder workspace page.
4. Save `git host + git username + personal access token` once.
5. Create the first workspace only after the builder page reports Git access as configured.

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
3. Argo runtime state in the build namespace:
   - `kubectl -n appliance-builds get workflows`
   - `kubectl -n appliance-builds get pods`
   - `kubectl -n appliance-builds logs <workspace-prepare-pod>`
4. Workspace storage state:
   - `kubectl -n appliance-builds get pvc,pv`
   - inspect `/var/lib/zon/workspaces/<workspace-name>` on the host

If the workflow is still running, later `GET /api/v1/workspaces` or
`GET /api/v1/current-workspace` calls will trigger reconciliation and move the
workspace from `pending` to `ready` or `failed`.

## Maintenance Rule

Whenever a UI route is added, removed, or changed, and whenever a UI handler
starts calling a different control-plane API route or method, update this
document in the same change.

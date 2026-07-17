# UI To Control-Plane Route Mapping

This document is the operator-facing map between browser-visible UI routes and
the downstream control-plane API calls the UI service makes on the browser's
behalf.

The key architectural rule is:

- the browser talks to the UI service
- the UI service talks to the control-plane API
- machine clients should use the control-plane API directly

This means browser devtools will often show only a UI route such as
`POST /builder/current-workspace`, while the actual appliance business action is
a separate server-side call from the UI service to the control plane such as
`POST /api/v1/current-workspace`.

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

These trace events are written by the UI service itself, so operators should
look in the UI service logs first:

- `/var/log/appliance/ui/application.log`
- `kubectl logs deploy/control-plane-ui -n appliance-system`
- `/var/log/appliance/ui/stdout.log`

The control plane now writes its own redacted API exchange logs too. For the
same browser action, operators can also inspect:

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
| `GET /builder/workspaces` | `builderPageData` | Session check/refresh as above; `GET /version`; `GET /health/ready`; `GET /api/v1/work-profiles`; `GET /api/v1/workspaces`; `GET /api/v1/current-workspace` | `200` full HTML page |
| `POST /builder/workspaces` | `createBuilderWorkspace` | Session check/refresh; `GET /api/v1/workspaces`; then either `POST /api/v1/current-workspace` for an existing same-name/same-profile workspace, or `POST /api/v1/workspaces` to create a new one | `303` redirect to `/builder/workspaces` |
| `POST /builder/current-workspace` | `setBuilderCurrentWorkspace` | Session check/refresh; `POST /api/v1/current-workspace` | `303` redirect to `/builder/workspaces` |
| `POST /builder/workspaces/delete` | `deleteBuilderWorkspace` | Session check/refresh; `DELETE /api/v1/workspaces/{workspaceId}` | `303` redirect to `/builder/workspaces?workspace_id=new` |
| `GET /partials/builder/work-profile` | `builderWorkProfilePartial` | Session check/refresh; `GET /api/v1/work-profiles` | `200` HTML partial |

## Operator Debugging Notes

### Example: `POST /builder/current-workspace`

When the browser shows:

- `POST /builder/current-workspace`
- response code `303`

that means the browser only observed the UI route. On the successful path, the
UI handler has already completed:

1. a server-side `POST /api/v1/current-workspace` call to the control plane
2. a successful `200` response from that control-plane API
3. a browser redirect back to `/builder/workspaces`

If the downstream control-plane call fails, the UI handler does **not** return
`303`. It renders the builder page with an error message instead.

So when browser devtools only show `303`, that is the UI service's response to
the browser, not the control-plane API response. The control-plane response is
visible in the UI service `control plane API call` log entry and in the control
plane `http api exchange` log entry.

### Where To Look First

To answer "did the request reach the control plane API or stop in the UI
service?":

1. Check the browser-visible UI route and response code.
2. Check the UI service trace log event for the downstream control-plane call.
3. Check the control-plane `http api exchange` log entry for the matching
   request path and request ID.
4. If needed, check durable state such as the `current_workspaces` table for
   workspace selection.

## Maintenance Rule

Whenever a UI route is added, removed, or changed, and whenever a UI handler
starts calling a different control-plane API route or method, update this
document in the same change.

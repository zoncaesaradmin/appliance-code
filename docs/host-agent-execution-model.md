# Host Agent Execution Model

## Intent

The host agent exists to expose host-scoped capability through the appliance API while keeping the appliance control plane as the external policy boundary. The pod is not meant to be the place where host facts are derived or where host-management commands truly execute.

The target model is:

- external callers talk only to the appliance API
- the control plane enforces authentication, authorization, and routing
- the host agent pod exposes an internal service API inside K3s
- host-scoped data collection and host-scoped command execution happen on the host OS context
- the pod acts as a transport and shaping layer, not as the host execution environment

## Current State

Today the host agent does **not** execute on the host.

It currently:

- runs as a normal non-root container in K3s
- mounts the host root at `/host`
- reads selected host files such as `/host/etc/os-release` and `/host/proc/...`
- returns those values through its internal HTTP API

This is a practical bootstrap path, but it is not the intended long-term execution boundary.

## Requirement

For host-agent features, the product requirement is:

- host-information gathering should happen on the host
- host-management commands should run on the host
- the K3s pod should proxy or broker those operations rather than reimplement them from container-local state

This avoids a design that grows by adding more per-field file scraping inside the pod.

## Preferred Design

## Host execution model (wifi-ap and mDNS)

The management WiFi access point is applied only through `appliance-host-agentd`
(`PUT /internal/v1/host/wifi-ap`). Install-time enablement with
Install stages offline host packages from the super-set bundle; day-2 Admin UI/API calls that same
API over the host agent Unix socket. Control-plane routes mirror it:
`GET|PUT /api/v1/host/wifi-ap` (permissions `host.read` / `host.write`).

Host mDNS (`avahi-daemon`) is applied through the same host-agentd path:
`GET|PUT /internal/v1/host/mdns`, mirrored as `GET|PUT /api/v1/host/mdns`.
Enabling without offline mDNS packages yields soft status `packages_missing`.
The status payload includes the advertised host mDNS name in `hostname.local`
form so the Admin UI/API can show the exact browser/discovery name in use.
Admin UI **Host Services** (`/admin/host-services`) is the day-2 configuration
surface for both host features. Management AP browser access is
`https://manage.ap/` (fixed A record `manage.ap` → `10.42.0.1` in CoreDNS when
installed; AP-local dnsmasq serves the same name when host `:53` is free) or
`https://10.42.0.1/`. Both names are install-time TLS SANs.

### Shape

- `appliance-host-agent` remains the in-cluster HTTP service behind the control plane
- a pinned host-side component such as `appliance-host-agentd` runs on the appliance host
- the pod communicates with the host-side agent over a local bridge, preferably a Unix domain socket mounted into the pod from a fixed host path such as `/run/zon/host-agent/agent.sock`
- the host-side agent performs host-scoped reads and host-scoped commands, then returns structured results

### Why this is preferred

- keeps the host agent API in K3s and therefore still addable and manageable through the appliance platform
- keeps true host execution on the host OS
- avoids needing a privileged pod, `hostPID`, `nsenter`, or broad host namespace escape in the normal service container
- allows explicit lifecycle ownership through `zonctl` and the appliance install/upgrade path
- supports allowlisted operations instead of a generic shell tunnel

## Non-Preferred Design

The following options are possible but not preferred as the baseline:

- parsing `hostnamectl` or other human-oriented CLI output directly in the pod
- adding more direct host-root file scraping in the pod for each new field
- running a privileged pod and using `nsenter` as the default mechanism for host execution
- exposing a generic arbitrary shell execution API from K3s into the host

These approaches are either brittle, too broad in privilege, or too easy to let sprawl.

## Host Agent Contract

The host-side bridge should expose a small typed contract, for example:

- `GetInfo`
- `GetStats`
- `GetHealth`
- future explicit host operations such as service checks or controlled diagnostics

It should not begin with a generic `RunCommand(string)` API.

The host-side component may internally use the best host-native mechanism for each operation, such as:

- direct reads from host kernel and OS interfaces
- host-native syscalls
- host-native commands where that is the authoritative source

The key boundary is that those actions execute in the host OS context, not in the container context.

## Security Expectations

- the host-side bridge is a sensitive boundary and must be explicitly documented
- only the control plane and internal services talk to it; it is never directly exposed externally
- requests must be allowlisted and typed
- audit logs must record operation, caller identity propagated from the control plane context where applicable, target scope, exit status, and duration
- secrets and sensitive command arguments must be redacted
- timeouts, cancellation, and partial-failure behavior must be explicit

## Lifecycle Ownership

- `zonctl` should own installation, upgrade, repair, and removal of the host-side component
- the release bundle must carry the pinned host-side artifact
- the chart should mount only what the pod needs for the bridge, ideally the socket directory and its own logs, not the full host root once the bridge exists

## Transition Plan

1. Define a host-runner interface inside the host agent so HTTP handlers call a host bridge rather than reading files directly.
2. Keep the current mounted-host collector only as a transitional implementation behind that interface.
3. Add the pinned host-side agent and a Unix-socket transport.
4. Switch info, stats, and health to the host-side implementation.
5. Remove broad host-root dependence from the pod once no longer needed.

## Decision

Until the bridge exists, the current implementation should be treated as a bootstrap compatibility path, not as the target architecture.

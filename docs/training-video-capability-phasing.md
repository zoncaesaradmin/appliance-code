# Training Video Capability Phasing

This note captures the rollout split for the optional training video library
capability. Implementation naming uses `video` for capability, module, API
paths, and permissions. The product-facing profile is `training` (core plus
video). Jellyfin is the first backend behind `video-runtime`; it is not part
of the capability name.

## Decisions (Slice A)

- **Surface:** video library browse/upload and playback APIs gated by
  capability `video`
- **Profile:** `training` = core capabilities + `video` (no builder/DNS
  unions in Slice A)
- **Images:** a separate future capability; do not overload `video`
- **Backend:** Jellyfin for Slice B; product names stay runtime-agnostic
  behind `video-gateway`

## Naming

| Layer | Name | Meaning |
|---|---|---|
| Capability | `video` | Store and stream training video on this appliance |
| Module | `video-runtime` | Cluster service: gateway + media runtime |
| Profile | `training` | Core appliance + video library/player |
| Stable in-cluster URL | `http://video-gateway.video.svc.cluster.local:8096` | Swap backends without changing the control plane |
| Operator library API | `/api/v1/video/library` | Manage UI upload/list/stream (CP, files-like) |
| Gateway stubs | `/video/v1/*` | Reserved module-proxy paths (not Manage UI) |
| Permissions | `video.library.read`, `video.library.write`, `video.play`, `video.admin` | Browse; upload/manage; stream; administer runtime |

## Admin profile activate vs install-time pod

Selecting **training** under Admin → Profiles does **not** deploy the video
workload. Activation only records a desired profile and may report
`requiresRestart`. The `video` namespace pod appears only when:

1. The signed **`video` pack** is present (OCI `video-runtime` +
   `appliance-video` chart), and
2. **`zonctl install` / `upgrade`** runs with **profile `training`** so the
   installer preloads the image, owns `/data/zon/video/library`, and Helms
   `appliance-video`.

Until that release/install path runs, the UI can show capability metadata
from the catalog while no `video-gateway` pod exists.

The top-right header chip is the **login session** (username / auth method),
not the appliance profile. The appliance profile is shown separately once
Slice C UI is installed.

## Slice A — Capability / profile wiring (no workload yet)

Mirror LAN DNS Phase 1 / inference Slice A:

- Add `CapabilityVideo = "video"` and profile `training` in control-plane and
  `zonctl` productconfig catalogs
- Capability deps: `video` → `base` (and `host` in metadata YAML)
- Module `video-runtime`: `ExecutionModeClusterService`, stable `BaseURL`,
  stub proxy routes for library/play
- Metadata catalogs under `metadata-bundle/base/` + sync/embed
- Docs and schema enums so `training` is accepted and reported
- Tests: resolve profile/modules; non-video profiles do not enable the module

Slice A does **not** require a running video pod, chart, OCI image, or pack.

## Slice B — Jellyfin chart + install gates + proxy (implemented)

- Pack `video`: OCI `registry.local/video-runtime` + `appliance-video` chart
- `RequiredPacks(training) → video`
- Chart in namespace `video` (Service `video-gateway:8096`, library
  `/data/zon/video/library`, UID **10008**, fsGroup **20000**)
- zonctl install/upgrade: capability-gated preload/Helm, gateway URL +
  NetworkPolicy inject, refuse in-place video → non-video upgrades
- Control plane: fail-closed `videoGatewayBaseURL` when video is on;
  Traefik `/video/v1` via CP; UI catch-all excludes `/video`; CP egress to
  video ns:8096

## Slice C — Operator UX + library APIs (implemented)

- Control-plane **files-like** library at `/api/v1/video/library` on the
  shared host path `/data/zon/video/library` (mounted into CP when video is
  on). HTML5 playback uses authenticated stream URLs with Range support.
- Manage → **Videos** page (capability-gated): upload, list, delete, play.
- Header shows **Profile: `<id>`** distinct from the login session chip.
- Jellyfin remains the runtime pod; Manage UX does not depend on Jellyfin’s
  own auth for v1 browse/upload/play.

### How to use end-to-end

1. Build and publish a release that includes pack `video` (from the local
   Slice B/C trees).
2. Install or upgrade with profile **`training`** so zonctl installs
   `appliance-video`.
3. Confirm the pod: `kubectl -n video get pods,svc`.
4. Open the UI: header shows `Profile: training`; Manage → Videos to upload
   and play. Library files land under `/data/zon/video/library` on the host.

## Explicit non-goals (still deferred)

- Day-2 auto-install of the video Helm release when Admin activates
  `training`
- Full proxied Jellyfin web UI / transcoding-dependent playback
- `builder-training` / DNS unions
- Image gallery capability

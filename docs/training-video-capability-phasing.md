# Video Capability Phasing

This note captures the rollout split for the optional **video** capability.
Implementation naming uses `video` for capability, module, API paths, and
permissions. The first product-facing profile that enables it is `training`
(core plus video); other profiles may enable `video` later. Jellyfin is the
first backend behind `video-runtime`; it is not part of the capability name.

## Decisions (Slice A)

- **Surface:** video library browse/upload and playback APIs gated by
  capability `video`
- **Profile:** `training` = core capabilities + `video` (no builder/DNS
  unions in Slice A). Video is not training-exclusive.
- **Images:** a separate future capability; do not overload `video`
- **Backend:** Jellyfin for Slice B; product names stay runtime-agnostic
  behind `video-gateway`

## Naming

| Layer | Name | Meaning |
|---|---|---|
| Capability | `video` | Store and stream video on this appliance |
| Module | `video-runtime` | Cluster service: gateway + media runtime |
| Profile | `training` | First shipped profile that enables `video` |
| Stable in-cluster URL | `http://video-gateway.video.svc.cluster.local:8096` | Swap backends without changing the control plane |
| Operator library API | `/api/v1/video/library` | Manage UI upload/list/stream (CP, files-like) |
| Gateway stubs | `/video/v1/*` | Reserved module-proxy paths (not Manage UI) |
| Permissions | `video.library.read`, `video.library.write`, `video.play`, `video.admin` | Browse; upload/manage; stream; administer runtime |

## Admin profile activate vs install-time pod

Selecting a video-capable profile (for example **training**) under
Admin → Profiles does **not** deploy the video workload. Activation only
records a desired profile and may report `requiresRestart`. The `video`
namespace pod appears only when:

1. The signed **`video` pack** is present (OCI `video-runtime` +
   `appliance-video` chart), and
2. **`zonctl install` / `upgrade`** runs with a **video-capable profile** so
   the installer preloads the image, prepares the host library path, and Helms
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
- `RequiredPacks` for video-capable profiles → `video` (today: `training`)
- Chart in namespace `video` (Service `video-gateway:8096`, library host path,
  UID **10008**, fsGroup **20000**)
- zonctl install/upgrade: capability-gated preload/Helm, gateway URL +
  NetworkPolicy inject, refuse in-place video → non-video upgrades
- Control plane: fail-closed `videoGatewayBaseURL` when video is on;
  Traefik `/video/v1` via CP; UI catch-all excludes `/video`; CP egress to
  video ns:8096

## Slice C — Operator UX + library APIs (implemented)

- Control-plane **files-like** library at `/api/v1/video/library` when video is
  on. Uploads synchronously validate an MP4 container with H.264 video and
  optional AAC audio, then atomically store one ready-to-play copy. HTML5
  playback streams it as `video/mp4` with Range support; native browser
  playback uses a short-lived HttpOnly cookie scoped only to the stream path,
  so no bearer token appears in a media URL. No playback-time conversion or
  alternate resolutions are used.
- Manage → **Videos** page (capability-gated): upload, list, delete, play.
  Operator UI does not expose host filesystem paths.
- Header shows **Profile: `<id>`** distinct from the login session chip.
- Jellyfin remains the runtime pod; Manage UX does not depend on Jellyfin’s
  own auth for v1 browse/upload/play.

### How to use end-to-end

1. Build and publish a release that includes pack `video`.
2. Install or upgrade with a **video-capable** profile (today: **`training`**)
   so zonctl installs `appliance-video`.
3. Confirm the pod: `kubectl -n video get pods,svc`.
4. Open the UI: Manage → Videos to upload and play.

## Explicit non-goals (still deferred)

- Day-2 auto-install of the video Helm release when Admin activates a
  video-capable profile
- Full proxied Jellyfin web UI / transcoding-dependent playback
- Additional profiles beyond `training` that also enable `video`
- Image gallery capability

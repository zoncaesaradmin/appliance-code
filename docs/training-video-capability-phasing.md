# Training Video Capability Phasing

This note captures the rollout split for the optional training video library
capability. Implementation naming uses `video` for capability, module, API
paths, and permissions. The product-facing profile is `training` (core plus
video). Jellyfin is the planned first backend behind `video-runtime`; it is
not part of the capability name.

## Decisions (Slice A)

- **Surface:** video library browse/upload and playback APIs gated by
  capability `video`
- **Profile:** `training` = core capabilities + `video` (no builder/DNS
  unions in Slice A)
- **Images:** a separate future capability; do not overload `video`
- **Backend:** Jellyfin planned for Slice B; product names stay
  runtime-agnostic behind `video-gateway`

## Naming

| Layer | Name | Meaning |
|---|---|---|
| Capability | `video` | Store and stream training video on this appliance |
| Module | `video-runtime` | Cluster service: gateway + media runtime |
| Profile | `training` | Core appliance + video library/player |
| Stable in-cluster URL | `http://video-gateway.video.svc.cluster.local:8096` | Swap backends without changing the control plane |
| Public API (stub) | `/video/v1/*` | External clients; appliance Bearer / session |
| Permissions | `video.library.read`, `video.library.write`, `video.play`, `video.admin` | Browse; upload/manage; stream; administer runtime |

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

**Follow-on (UX):** proxied Jellyfin web UI for browse/play; library upload
+ scan trigger; thin Admin Training library page (v1.1).

## Explicit non-goals (Slice A)

- Running Jellyfin or any video pod
- Upload/play UI
- New signed pack or foundation size change
- `builder-training` / DNS unions
- Image gallery capability

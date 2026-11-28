# Geo-Zone appliance integration

This change establishes the appliance contract for the Geo-Zone product from
`zone-studio` without importing its standalone installer or its private
SuperTokens deployment. Geo-Zone is represented by one delivery pack and
granular capabilities:

- `geo-spatial-store` — PostGIS-backed boundary persistence and migrations.
- `geo-data-api` — authenticated spatial queries and GeoJSON export.
- `geo-basemap` — appliance-owned, offline map tiles/styles and attribution.
- `geo-zone-editor` — the MapLibre/Terra Draw editor and its static assets.

The `geo-zone` profile selects all four capabilities plus the shared `base` and
`application-auth` capabilities. `geo-stack` is the sole delivery pack for the
Geo-Zone-specific artifacts; it is intentionally not split into one pack per
service.

The Geo employee entrypoint and Zone Studio editor are now delivered as a
second static entrypoint in the shared appliance UI image. `/geo/login` uses
the appliance's local username/password session API and `/geo` serves the
MapLibre/Terra Draw editor. Geo data requests remain behind the control-plane
`/api/v1/geo/*` proxy and are authorized with `geo.read` and `geo.write`.

The Geo runtime workloads run in the Kubernetes namespace `ace-app-geo`, and
the service endpoint is `geo-data-api.ace-app-geo.svc.cluster.local`. Existing
installations that still use the former `geo` namespace require an explicit,
data-preserving migration before upgrading; changing this value alone must not
be used as an in-place storage migration.

Runtime images, the PostGIS chart/migrations, the data API, and the offline
basemap must still be added as signed release-input artifacts before the Geo
profile is offered for a production install. Until then the editor is a
real, authenticated UI shell with a local coordinate-grid fallback; its
persistence API is intentionally fail-closed when the Geo data service is
absent. The fallback is not a substitute for the signed geographic basemap
pack described below.

## User flow

1. Install or activate the `geo-zone` profile with the signed `geo-stack`
   pack present.
2. Complete normal appliance setup and create the first local administrator.
3. Open `/geo/login` and sign in with that local username/password. A
   successful session lands at `/geo`; `/console/login` remains the separate
   appliance-operations entrypoint and `/app/login` remains the OTP-based
   application entrypoint.
4. The built-in administrator already has `geo.read` and `geo.write`. For
   other employees, assign those permissions through a local role before
   granting them Geo access.

The UI and login boundary are therefore usable before the persistence service
is shipped, while missing Geo runtime artifacts cannot silently become an
internet-backed or unauthenticated fallback.

The standalone Zone Studio catalog remains useful as the source inventory, but
its `ingress`, `auth-service`, and `backups` components map to appliance-owned
concerns: Traefik, the common foundation Auth Service, and the appliance
recovery-set/backup contract. They are not duplicated as Geo-specific packs.

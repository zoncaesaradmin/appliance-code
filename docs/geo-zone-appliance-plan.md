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

The first implementation slice is metadata and routing only. Runtime images,
the PostGIS chart/migrations, the data API, and the offline basemap must be
added as signed release-input artifacts before `geo-zone` is offered for a
production install. This keeps the profile fail-closed instead of shipping an
apparently enabled profile with an online basemap or an unverified image.

The standalone Zone Studio catalog remains useful as the source inventory, but
its `ingress`, `auth-service`, and `backups` components map to appliance-owned
concerns: Traefik, the common foundation Auth Service, and the appliance
recovery-set/backup contract. They are not duplicated as Geo-specific packs.

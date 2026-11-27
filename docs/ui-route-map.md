# Appliance UI route map

This document records browser-visible UI boundaries and the control-plane
authentication calls behind them. UI pages remain in the control-plane UI
image; product APIs remain owned and authorized by the control plane.

| Browser route | UI entry | Authentication | Downstream API | Result |
| --- | --- | --- | --- | --- |
| `/console/login` | Control-plane React entry | `POST /api/v1/auth/login` with the local appliance domain | Control-plane session and operator APIs | Operator console at `/home` and its management routes |
| `/app/login` | Application React entry | `/api/app/v1/auth/otp/*` | Application identity/authentication services | Customer/member workspace at `/app` |
| `/app` Explore nearby dialog | `NearbyJoinDialog` (`ApplicationWorkspacePage`) | browser geolocation; map pin + zoom | `POST /api/app/v1/onboarding/nearby`, `POST /api/app/v1/onboarding/join` | Auto-searches near the device, then allows **Search near me** or move the pin → **Search from pinned location** |
| `/app/discover` | `NearbyDiscoveryPage` | same two location workflows | `POST /api/app/v1/onboarding/nearby`, join, `POST /api/app/v1/onboarding/zone-requests` | Full-page discovery with current-location and map-pin search |
| `/geo/login` | Geo-Zone entry | `POST /api/v1/auth/login` with the local appliance domain | Control-plane session, then `/api/v1/geo/*` | Employee Geo workspace at `/geo` |
| `/geo` | Zone Studio editor entry | Existing control-plane bearer session | `/api/v1/geo/*`, authorized by `geo.read`/`geo.write` | Map editor, saved zones, and GeoJSON export |

Application OTP and onboarding calls use the `/api/app/v1/...` namespace. Auth
service principal resolution is an internal service-to-service call at
`/internal/v1/auth/principal` and is not exposed through the public ingress.

The Geo editor is a second static entrypoint in the shared UI image. It does
not add a second user store or SuperTokens deployment. The browser sends the
existing access token to the control-plane Geo proxy; the Geo service must
never trust caller-supplied identity headers from an untrusted network.

The built-in administrator role already includes `geo.read` and `geo.write`,
so the appliance administrator can use `/geo/login` immediately. Other local
users need a role that grants the Geo permissions; the Geo login page itself
does not create a separate user or role database.

The Geo page currently uses a local MapLibre background style so it does not
require public map tiles at runtime. The signed Geo stack must provide the
appliance-owned basemap assets before geographic basemap data is enabled.

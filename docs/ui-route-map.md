# Appliance UI route map

This document records browser-visible UI boundaries and the control-plane
authentication calls behind them. UI pages remain in the control-plane UI
image; product APIs remain owned and authorized by the control plane.

| Browser route | UI entry | Authentication | Downstream API | Result |
| --- | --- | --- | --- | --- |
| `/console/login` | Control-plane React entry | `POST /api/v1/auth/login` with the local appliance domain | Control-plane session and operator APIs | Operator console at `/home` and its management routes |
| `/app/login` | Application React entry | `/api/app/v1/auth/otp/*` | Application identity/authentication services | Customer/member workspace at `/app` |
| `/app` Explore nearby dialog | `NearbyJoinDialog` (`ApplicationWorkspacePage`) | browser geolocation; map pin + zoom | `POST /api/app/v1/onboarding/nearby`, `POST /api/app/v1/onboarding/join` | Auto-searches near the device, confirms the current location on the map, then allows choosing another pin → **Search from pinned location** |
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

The Geo page uses the Zone Studio OpenFreeMap Bright style when no style is
configured, preserving the geographic map used by the original POC. A signed
Geo basemap pack can provide `VITE_MAP_STYLE` with an appliance-owned style and
local tile assets. `VITE_MAP_STYLE=offline` selects the coordinate-grid
fallback for development and tests.

The application discovery map uses that same `VITE_MAP_STYLE` contract. Once
the browser has a current location, the map is centered there immediately and
shows the current-location marker. Users can move the pin and explicitly
search from the pinned location; the UI does not ask them to search from the
current location again while it is already showing that location.

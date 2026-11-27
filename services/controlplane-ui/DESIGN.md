# DESIGN.md - Zonited application entry

## Context

- Artifact type: onboarding/auth flow and installable local PWA shell
- Positioning: trusted, local, quietly distinctive
- Audience: Zonited application users | Primary action: sign in and choose a service or explore nearby Zonities
- Adjectives: local, calm, human, precise
- Aesthetic essence: neighbourhood signal, warm paper, quiet confidence
- Mode: light | Density: balanced
- Constraints: React/Vite, keep operator authentication separate from application identity; discovery map tiles are an onboarding-only online enhancement and the signed-in shell remains usable without them

## Aesthetic

- Direction: editorial service index rather than an appliance dashboard
- Defining trait: a quiet service index with a single strong “explore nearby” action
- Signature move: numbered service cards that turn the landing page into a simple local directory

## Typography and color

- Display/body: existing bundled system stack for offline operation; avoid fetching fonts at runtime
- Base: 16px with compact 1.25 headings and 1.6 body line-height
- Palette: Burgundy `#7F1D3D`, Fresh Sage `#83AD8B`, Warm Cream `#FFF9F1`, Espresso `#3B2C2C`, White `#FFFFFF`, Warm Sand `#EAD9C6`, Pale Sage `#E7F0E5`, Deep Sage `#496D50`, Soft Amber `#E6AC49`, with semantic error `#B42318`
- Distribution: 65–75% Cream / White surfaces, 15–20% Burgundy identity, 5–10% Sage, with small functional accents
- Brand rule: **Zonited** is the platform; **Zonity** is an individual geo-mapped community. Use `microzone` in UI copy when describing the community concept.

## Layout and components

- Login uses a Neighborhood Canvas: a bundled full-bleed Zonity-zone illustration
  fills the viewport with no story card or wordmark overlay, and the
  authentication controls remain in a focused warm-cream panel on the right.
  On narrow screens the artwork becomes a short header above the form.
  Service-ring and icon colors must match brand burgundy `#7F1D3D`.
- Login artwork is a release asset, not a network dependency. It must remain
  available from the offline static-shell cache and must not load from
  `zonited.com` or another public origin. Prefer a product-grade digital
  composition over the marketing-site neighborhood painting, with no story
  card or wordmark overlay so the artwork stays fully visible.
- The application shell has no operator-console, account, or administration links. Application and operator URL spaces remain separate.
- Inputs retain visible labels, focus rings, keyboard operation, and inline errors.
- Mobile OTP entry separates country selection from the local number; India (+91) is the default and the local number is digit-only with country-specific length validation.
- Authentication actions use full-width, vertically separated primary and secondary buttons so the code step has a clear next action and a calm recovery path.
- Motion is limited to opacity/transform and respects reduced-motion preferences.
- Nearby discovery shows the searched point and returned Zonity points on an OpenStreetMap tile view. Users can choose another point on the map before searching again; map failure never blocks the platform-managed Zonity request path.

## PWA and security

- Manifest starts at `/app` and service worker caches only the static shell.
- `/api/`, `/version`, and health endpoints are never cached.
- Application login never calls the operator `/api/v1/auth/login` contract. It
  uses the separate application identity service's local/test mobile OTP
  contract; it must never fall back to appliance operator credentials.
- `/app` is the customer-oriented application entrypoint. Future
  customer, vendor, and zone-administration entrypoints select an application
  audience/channel, but a route is not a permission grant. Their APIs must
  enforce the resolved user, tenant, role, service, and zone scope server-side.
- The current PWA knows only application-session validity. It must not present
  a placeholder identity as proof of customer, vendor, or administrator
  authority until the generic application-principal contract is implemented.

## Changelog

- 2026-11-23: Added the application-context login, protected PWA shell, and offline static-shell metadata.
- 2026-11-23: Applied the Zonited brand palette and supplied logo assets to the application login and PWA shell.
- 2026-11-23: Removed the operator credential fallback from application login; mobile identity is the only application-login contract.
- 2026-11-24: Reworked the signed-in application shell into a service index, removed operator-console escape links, and switched the shell mark to the supplied Zonited logo.
- 2026-11-24: Refined mobile OTP entry with a country selector, digit-only local-number input, and separated action buttons.
- 2026-11-24: Simplified the application login visual system by removing the decorative circle, using the square Zonited mark, compacting country-code selection, removing redundant phone guidance, and centering the primary action.
- 2026-11-24: Replaced the login story footer slogans with a single Zonited Solutions attribution.
- 2026-11-25: Included public brand assets in the production image and refined the login logo scale and vertical alignment.
- 2026-11-25: Added the application logo to the offline shell so the sign-in identity remains visible in the PWA cache.
- 2026-11-25: Made the application home membership-aware, showing compact active Zonity access and a location-based join dialog before service tabs become available.
- 2026-11-25: Added coordinate-aware nearby results, an onboarding map with OpenStreetMap attribution, searched-location confirmation, and a choose-another-location flow.
- 2026-11-26: Replaced the full-height split login with the Neighborhood Canvas,
  using a login-specific bundled illustration, floating authentication panel,
  and responsive artwork header while preserving the OTP controls.
- 2026-11-27: Replaced the login hero with a digital Zonity-map artwork so the
  app entry stays distinct from the zonited.com marketing neighborhood painting.
- 2026-11-27: Switched the login canvas to layered product-preview artwork,
  lightened the story plate, and removed the long support sentence under the
  benefit headline.
- 2026-11-27: Moved the login canvas to the full-bleed Zonity-zone artwork while
  keeping the light story plate.
- 2026-11-27: Anchored the story plate to the bottom-left and cropped the canvas
  so the Zonity service ring stays clear above it.
- 2026-11-27: Removed the login story card and placed only the transparent
  Zonited wordmark on the canvas.
- 2026-11-27: Removed the temporary wordmark overlay and refreshed the Zonity
  canvas so the service ring uses brand burgundy instead of a lighter rose.
- 2026-11-27: Updated the login neighborhood canvas to the mixed apartment /
  shop / godown / house composition without radiating service lines, with
  restrained burgundy accents only on the ring and a few building edges.

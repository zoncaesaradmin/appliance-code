# DESIGN.md - Zonited application entry

## Context

- Artifact type: onboarding/auth flow and installable local PWA shell
- Positioning: trusted, local, quietly distinctive
- Audience: Zonited application users | Primary action: sign in and choose a service or explore nearby Zonities
- Adjectives: local, calm, human, precise
- Aesthetic essence: neighbourhood signal, warm paper, quiet confidence
- Mode: light | Density: balanced
- Constraints: React/Vite, air-gapped runtime, keep operator authentication separate from application identity

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

- Login keeps the split story/form layout; the signed-in app uses a spacious service index with three service tabs.
- The application shell has no operator-console, account, or administration links. Application and operator URL spaces remain separate.
- Inputs retain visible labels, focus rings, keyboard operation, and inline errors.
- Mobile OTP entry separates country selection from the local number; India (+91) is the default and the local number is digit-only with country-specific length validation.
- Authentication actions use full-width, vertically separated primary and secondary buttons so the code step has a clear next action and a calm recovery path.
- Motion is limited to opacity/transform and respects reduced-motion preferences.

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

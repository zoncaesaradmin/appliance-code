# DESIGN.md - Zonited application entry

## Context

- Artifact type: onboarding/auth flow and installable local PWA shell
- Positioning: trusted, local, quietly distinctive
- Audience: Zonited application users | Primary action: sign in and enter their microzone workspace
- Adjectives: local, calm, human, precise
- Aesthetic essence: neighbourhood signal, warm paper, quiet confidence
- Mode: light | Density: balanced
- Constraints: React/Vite, air-gapped runtime, keep operator authentication separate from application identity

## Aesthetic

- Direction: editorial utility rather than an operator dashboard
- Defining trait: a strong left-side “microzone signal” anchors the login decision
- Signature move: three connected signal nodes that turn the Zonited mark into a local-place motif

## Typography and color

- Display/body: existing bundled system stack for offline operation; avoid fetching fonts at runtime
- Base: 16px with compact 1.25 headings and 1.6 body line-height
- Palette: Burgundy `#7F1D3D`, Fresh Sage `#83AD8B`, Warm Cream `#FFF9F1`, Espresso `#3B2C2C`, White `#FFFFFF`, Warm Sand `#EAD9C6`, Pale Sage `#E7F0E5`, Deep Sage `#496D50`, Soft Amber `#E6AC49`, with semantic error `#B42318`
- Distribution: 65–75% Cream / White surfaces, 15–20% Burgundy identity, 5–10% Sage, with small functional accents
- Brand rule: **Zonited** is the platform; **Zonity** is an individual geo-mapped community. Use `microzone` in UI copy when describing the community concept.

## Layout and components

- Desktop split layout with a story rail and a focused form panel; stacked on narrow screens.
- Primary action is filled and full-width; console access is a low-emphasis footer action.
- Inputs retain visible labels, focus rings, keyboard operation, and inline errors.
- Motion is limited to opacity/transform and respects reduced-motion preferences.

## PWA and security

- Manifest starts at `/app` and service worker caches only the static shell.
- `/api/`, `/version`, and health endpoints are never cached.
- Application login never calls the operator `/api/v1/auth/login` contract. Until the mobile-identity service is
  integrated, `/app/login` is an explicit disabled state rather than a username/password fallback.

## Changelog

- 2026-11-23: Added the application-context login, protected PWA shell, and offline static-shell metadata.
- 2026-11-23: Applied the Zonited brand palette and supplied logo assets to the application login and PWA shell.
- 2026-11-23: Removed the operator credential fallback from application login; mobile identity is now the required future contract.

# DESIGN.md - Zonity application entry

## Context

- Artifact type: onboarding/auth flow and installable local PWA shell
- Positioning: trusted, local, quietly distinctive
- Audience: Zonity application users | Primary action: sign in and enter their microzone workspace
- Adjectives: local, calm, human, precise
- Aesthetic essence: neighbourhood signal, warm paper, quiet confidence
- Mode: light | Density: balanced
- Constraints: React/Vite, air-gapped runtime, reuse the existing authenticated control-plane API

## Aesthetic

- Direction: editorial utility rather than an operator dashboard
- Defining trait: a strong left-side “microzone signal” anchors the login decision
- Signature move: three connected signal nodes that turn the Zonity mark into a local-place motif

## Typography and color

- Display/body: existing bundled system stack for offline operation; avoid fetching fonts at runtime
- Base: 16px with compact 1.25 headings and 1.6 body line-height
- Palette: warm paper `#f4f0e7`, deep moss `#17352d`, ink `#1d2924`, muted sage `#6b766e`, saffron accent `#f4c95d`, error `#a33a32`
- Distribution: 60 neutral / 30 brand / 10 accent

## Layout and components

- Desktop split layout with a story rail and a focused form panel; stacked on narrow screens.
- Primary action is filled and full-width; console access is a low-emphasis footer action.
- Inputs retain visible labels, focus rings, keyboard operation, and inline errors.
- Motion is limited to opacity/transform and respects reduced-motion preferences.

## PWA and security

- Manifest starts at `/app` and service worker caches only the static shell.
- `/api/`, `/version`, and health endpoints are never cached.
- Authentication continues through `/api/v1/auth/login`; no second credential store or external network dependency is introduced.

## Changelog

- 2026-11-23: Added the application-context login, protected PWA shell, and offline static-shell metadata.

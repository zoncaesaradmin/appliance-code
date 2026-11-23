# Zon appliance project guidance

## Product naming

- **Zon Appliance** is the operator-facing appliance and operations console.
- **Zonited** is the application platform and brand.
- A **Zonity** is an individual geo-mapped community. Do not use “Zonity” as
  the product name.

## Zonited brand palette

The canonical source is the maintained brand specification at
`~/zonited/zonited-brand-colors.md`. Keep these values in the UI design tokens
so future pages remain usable in an air-gapped build without fetching a theme
or font at runtime:

| Token | Value | Use |
| --- | --- | --- |
| `brand.primary` | `#7F1D3D` | Burgundy wordmark, headings, primary actions |
| `brand.secondary` | `#83AD8B` | Fresh Sage illustrations, icons, large accents |
| `brand.background` | `#FFF9F1` | Warm Cream page backgrounds |
| `text.primary` | `#3B2C2C` | Espresso body and primary text |
| `surface.default` | `#FFFFFF` | Cards, forms, tables |
| `surface.warm` | `#EAD9C6` | Borders, dividers, warm sections |
| `surface.sage` | `#E7F0E5` | Selected and subtle sage surfaces |
| `action.sage` | `#496D50` | Accessible sage text and controls |
| `accent.amber` | `#E6AC49` | Sparing decorative highlights |
| `state.success` | `#496D50` | Success states |
| `state.warning` | `#805300` | Warning states |
| `state.error` | `#B42318` | Error states |
| `state.info` | `#285EA8` | Informational states |

Use white text on Burgundy or Deep Sage. Do not use Fresh Sage for normal-size
text on Cream or White. Semantic colors are functional and must not be
replaced with Burgundy merely because it is the primary brand color.

The application UI owns this palette. The operator console may retain its
separate operational palette until a deliberate unification decision is made.
When adding an application page, extend the application tokens in
`services/controlplane-ui/src/styles.css` and update
`services/controlplane-ui/DESIGN.md` if the visual system changes.

Application identity is separate from appliance identity. Application pages
must never submit `/api/v1/auth/login`, accept operator usernames/passwords, or
reuse an operator session as an application session. The planned application
session contract is `domain=application` with `authMethod=mobile_otp`; until
that service exists, `/app/login` must remain an explicit disabled state.

The bundled application brand assets are
`services/controlplane-ui/public/zonited-logo-circle.png` for compact UI
identity and `services/controlplane-ui/public/zonited-logo-square.png` for
PWA/icon contexts. Do not substitute the operator mark on application pages.

# Appliance Profiles V1

The appliance ships as one complete signed offline bundle. A profile selects
the capabilities that the control plane enables at startup; it does not select
or download a different bundle.

## Capabilities

| Capability | Purpose |
| --- | --- |
| `base` | Health/version, authentication, users, roles, tokens, files, and the shared MCP transport. |
| `lan-discovery` | Host-level mDNS advertisement for same-network discovery. |
| `files` | Authenticated appliance file spaces backed by the foundation blob store. |
| `artifact` | OCI registry APIs backed by the local Artifact Server. |
| `dns` | Appliance-owned LAN DNS backed by CoreDNS. |
| `inference` | Local OpenAI-compatible inference APIs. |
| `video` | Video library storage and playback. |
| `applications` | User-managed application lifecycle through the host agent. |
| `plaintext-http` | Optional HTTP ingress for profiles that explicitly require it. |

Every capability depends on `base`; additional dependencies are declared in
the signed metadata bundle and validated before the public listener starts.

## Profiles

The current signed catalog defines these profiles:

| Profile | Capabilities |
| --- | --- |
| `core` | `base`, `actions`, `lan-discovery`, `files` |
| `application-core` | `base`, `commerce`, `channel-integrations`, `application-identity`, `application-auth` |
| `storage-landns` | `base`, `actions`, `lan-discovery`, `files`, `artifact`, `dns` |
| `training` | `base`, `actions`, `lan-discovery`, `host`, `files`, `video`, `guest-access`, `focus-content`, `applications`, `plaintext-http` |
| `private-ai` | `base`, `actions`, `lan-discovery`, `inference` |
| `private-ai-webui` | `base`, `actions`, `lan-discovery`, `inference`, `open-webui` |

The authoritative profile, capability, package, and module definitions are the
signed metadata catalogs under `metadata-bundle/base/`. Installers must use
those catalogs rather than duplicating profile logic.

# Appliance metadata bundle (product content)

This tree is the **only place to author** the base metadata-bundle YAML and
section files that ship as `appliance-metadata-bundle-X.Y.Z.N.tar.zst`.

| Path | Role |
|------|------|
| `base/` | Source of truth for profiles, capabilities, activation, UI, notifications, MCP tools, and `bundle.yaml` |
| (packaged) | `scripts/package/generate-metadata-bundle.sh` builds the signed release archive from `base/` |

## Separation from Go

Control-plane logic that *loads*, *validates*, *installs*, and *rolls back*
metadata bundles lives under:

```text
services/controlplane/internal/metadatabundle/   # Go only
```

Do **not** put product catalog YAML under that package for editing.

Because Go `//go:embed` cannot reference files outside the controlplane module,
a **generated** byte-identical snapshot is kept for offline fallback
materialization:

```text
services/controlplane/internal/metadatabundle/embedded/   # generated
```

After changing `base/`, sync and commit the snapshot:

```bash
./scripts/package/sync-embedded-metadata-bundle.sh
```

`make build` syncs automatically; `make verify` runs `--check` so drift fails closed.

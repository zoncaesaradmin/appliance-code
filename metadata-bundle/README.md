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

Metadata is read from files at startup, not compiled into Go. There is no
generated source copy, embedding step, or synchronization command.

## Local Development

Run the development binary from this repository (including a service
subdirectory). It finds `metadata-bundle/base/` by walking up from the working
directory. When running elsewhere, set `APPLIANCE_DEVELOPMENT_METADATA_DIR` to
the absolute path of that directory. This override is development-only.

Edit YAML here and restart the control plane and, when running separately,
the automation runtime. No Go rebuild or copy step is needed. Configuration
is a startup snapshot: editing files does not hot-reload API routes or partially
rewire services. Missing files, invalid YAML, and invalid capability dependencies
fail startup. Old development copies under the service data directory are not used.

## Installed Appliances

Release binaries have no repository fallback. The control plane reads the
version-matched base tree at
`<dataDir>/metadata-bundles/appliance-metadata-bundle-X.Y.Z.0/`, already mounted
by the chart and staged by `zonctl` from the verified signed offline bundle.
Signature verification remains the installer's responsibility; the runtime
validates metadata schema and software compatibility before startup.

Do not edit installed signed trees in place. Publish the changed metadata via
the signed offline release flow and restart the affected services. YAML-only
changes do not require recompiling Go, but still require packaging and signing
the updated release input. Metadata archive installation/rollback APIs retain
their own explicit lifecycle; source-file edits are not an activation API.

`make verify` validates the canonical files through the file-loader and startup
tests, including restart-to-apply and production fail-closed behavior.

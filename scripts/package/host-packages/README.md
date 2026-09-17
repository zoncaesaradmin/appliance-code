Repo-owned offline host package payload for installer-owned host capabilities.

Expected layout after export (see `export-host-packages.sh`):

- `ubuntu/22.04/<arch>/*.deb`
- `ubuntu/24.04/<arch>/*.deb`

where `<arch>` is `TARGET_ARCH` (`amd64` or `arm64`, required (no default)).

Cross-arch export (e.g. arm64 packages on an amd64 build host) uses a temporary
apt sources.list against `archive.ubuntu.com` / `security.ubuntu.com` for the
target arch, because host mirrors are often amd64-only. Override with
`HOST_PACKAGES_APT_MIRROR` / `HOST_PACKAGES_APT_SECURITY_MIRROR` if needed.

`build-full-bundle` always exports the complete capability set (`mdns` +
`wifi-client` + `wifi-ap`) into appliance-code `.run/host-packages`. Release-input packaging
then copies that tree as signed `host-packages/`. Install stages packages
offline; enablement is day-2 only.

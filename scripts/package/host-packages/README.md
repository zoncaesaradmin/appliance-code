Repo-owned offline host package payload for installer-owned host capabilities.

Expected layout after export (see `export-host-packages.sh`):

- `ubuntu/22.04/amd64/*.deb`
- `ubuntu/24.04/amd64/*.deb`

`build-full-bundle` always exports the complete capability set (`mdns` +
`wifi-client` + `wifi-ap`) into appliance-code `.run/host-packages`. Release-input packaging
then copies that tree as signed `host-packages/`. Install stages packages
offline; enablement is day-2 only.

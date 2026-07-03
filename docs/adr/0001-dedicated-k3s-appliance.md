# ADR 0001: Dedicated K3s Appliance

- Status: Accepted
- Date: 2026-07-03

## Context

Supporting an arbitrary existing Kubernetes cluster would make CNI, ingress, storage, CRD ownership, upgrade, and uninstall behavior part of the first-release compatibility surface. The product is intended to behave as an appliance.

## Decision

V1 is a dedicated, product-managed, single-node K3s appliance.

- Production support starts with Ubuntu Server 24.04 LTS on `linux/amd64` and an `ext4` data filesystem.
- Darwin and `linux/arm64` remain supported development build targets, not production appliance targets, until their appliance conformance lanes exist.
- The installer owns the bundled pinned K3s installation, embedded etcd configuration, Traefik configuration, appliance namespaces, charts, systemd units, local data directories, and offline image-preload configuration it creates.
- The operator owns the host OS, OS patching, storage device, network, DNS, NTP, SSH access, firewall policy outside installer-declared ports, and off-appliance backup destination.
- The appliance does not support unrelated customer workloads in its K3s cluster in v1.
- K3s API access binds to the management host and is not exposed through product ingress. Product ingress exposes only HTTP/HTTPS on ports 80/443; SSH remains an operator concern.
- The installer performs a non-mutating preflight and requires explicit approval before installing K3s or changing host settings. It never performs OS updates.
- Uninstall preserves data by default. Factory reset and secure decommission are separate, explicit, destructive commands with confirmation and backup warnings.

Initial sizing classes:

- Artifact/control-plane use: initial minimum 4 vCPU, 8 GiB RAM, and 100 GiB data storage.
- Build-enabled use: initial minimum 8 vCPU, 16 GiB RAM, and 250 GiB data storage.
- Final sizing is validated against the pinned zot image/extension set and Buildah toolchain release and documented per release.

## Consequences

This gives the release repo authority to install and upgrade K3s predictably. Existing-cluster deployment, additional Linux distributions, ARM appliances, multi-node HA, and coexistence with unrelated workloads require separate compatibility work and a superseding or additional ADR.

## Verification

- Clean-host install and preflight tests
- Node reboot and power-loss recovery tests
- Uninstall-preserving-data and factory-reset tests
- Tests proving no appliance service except ingress is remotely exposed

## References

- [K3s requirements](https://docs.k3s.io/installation/requirements)
- [K3s air-gap installation](https://docs.k3s.io/installation/airgap)

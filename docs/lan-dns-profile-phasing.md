# LAN DNS Profile Phasing

This note captures the rollout split for the new DNS-related appliance
profiles.

## Phase 1: Profile And Capability Wiring

Phase 1 adds:

- internal capability name: `dns`
- product-facing profile names: `lan-dns`, `storage-lan-dns`
- centralized profile-to-capability mapping in the control plane and CLI
- install-time and verification-time gating based on the resolved capability
  set
- documentation and schema updates so the new profiles are accepted and
  reported consistently

Phase 1 does not add:

- a DNS server pod
- DNS-specific Kubernetes resources
- DNS-specific readiness or smoke checks
- a new external DNS API surface

## Phase 2: DNS Workload Delivery

Phase 2 delivers the appliance-owned LAN DNS data plane on the existing `dns`
capability (no new profile names):

- Helm chart `deploy/charts/appliance-dns` (CoreDNS, hostNetwork UDP/TCP 53)
- First-class bundle contract `dnsImage` / `dnsChart` / `compatibility.dnsVersion`
  with annotation `registry.local/coredns:bundled` and digest-pinned
  `registry.local/coredns@sha256:…`
- Capability-gated preload, Helm install before the control plane, and refuse
  in-place dns→non-dns upgrades
- Control-plane fail-closed `dnsReadyURL` when the dns capability is enabled
- Target verification: Deployment Ready + `dig @127.0.0.1` for the local zone;
  absence checks on non-dns profiles

Out of scope for this slice: DNS admin UI/API, multi-node HA, replacing
kube-system CoreDNS, and DHCP/router integration.

## Intent

The purpose of the split was to let product profile selection, installer
contracts, and release tooling stabilize first (Phase 1), then land the DNS
service as a separate vertical slice (Phase 2) on the same capability gate.

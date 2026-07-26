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

## Phase 3: API-driven LAN DNS records

Phase 3 makes the DNS appliance useful for other hosts on the LAN:

- Control-plane SQLite owns A records under `appliance.internal`
- Authenticated API: `GET/PUT/DELETE /api/v1/dns/records` (capability `dns`)
- Permissions: `dns.records.read`, `dns.records.write`, `dns.records.register`
- Zone sync patches CoreDNS ConfigMap `db.local`; CoreDNS `reload` serves it
- UI `/dns` for admin CRUD
- Peer publish (base capability on every appliance):
  `POST /api/v1/lan-dns/publish` with remote DNS URL + token + name + ipv4
  (permission `lan_dns.publish`). Installers never write DNS records.

Operator curl cookbook (setup, direct DNS API, peer publish):
`appliance-release` `docs/lan-dns-usage.md`.

Still out of scope: multi-node HA CoreDNS, replacing kube-system CoreDNS, and
DHCP/router integration.

## Intent

The purpose of the split was to let product profile selection, installer
contracts, and release tooling stabilize first (Phase 1), then land the DNS
service as a separate vertical slice (Phase 2) on the same capability gate.

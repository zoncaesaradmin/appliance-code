# ADR 0003: Trusted Buildah Build Boundary

- Status: Accepted with validation gate
- Date: 2026-07-03

## Context

Containerfile build instructions execute arbitrary code. Kubernetes pods and rootless builders reduce risk but do not isolate mutually hostile tenants from a shared kernel. Running rootless Buildah inside K3s also depends on the pinned kernel, user-namespace, seccomp, AppArmor, storage-driver, and OCI-runtime configuration.

## Decision

V1 supports trusted builds only. It is not a hostile multi-tenant build service.

- Buildah is the only supported image builder. Use a pinned Buildah image in ephemeral task pods within one appliance-generated Argo Workflow per build.
- The supported v1 input is a `Containerfile` from an HTTPS Git repository on an operator allowlist at an immutable commit SHA. A file literally named `Dockerfile` is accepted as Buildah-compatible input, but no separate toolchain or behavior is implied by that filename.
- The supported output is an OCI image pushed directly to zot. Produce OCI format unless a proven client-compatibility requirement forces another manifest format.
- Only authenticated principals with `builds.create` may submit builds. Administrators grant that permission only to trusted developers and automation identities.
- Do not mount a host runtime socket, host path, host namespace, control-plane service-account token, or long-lived registry credential.
- Run rootless and non-privileged. Prefer Buildah's OCI/rootless isolation. A chroot fallback, `/dev/fuse`, unconfined seccomp/AppArmor, `allowPrivilegeEscalation`, added capability, or user-namespace exception is not accepted by default and requires explicit threat-model evidence against the pinned K3s/host combination.
- If the supported Buildah configuration requires a privileged pod or node-equivalent access, reject that configuration and reopen this decision.
- Each build receives a dedicated service account with token automount disabled, an `emptyDir` workspace, short-lived source and zot credentials, a deadline, resource limits, bounded logs, and Workflow/pod cleanup TTL.
- Build ingress and egress are default-deny. Explicit egress permits cluster DNS, allowlisted source hosts, and zot. Revalidate DNS answers and redirects to reduce SSRF and rebinding risk.
- Build concurrency defaults to one on the single-node appliance and is configurable only within capacity-tested bounds.
- Buildah local storage and cache are non-authoritative and disposable. Start without a shared persistent cache; define cache quotas, cleanup, ownership mapping, and restore behavior before enabling one.
- Use an isolated `REGISTRY_AUTH_FILE` supplied as a short-lived file, never credentials on command arguments. Delete it and the Buildah storage root when the Job ends.
- Record requester, token ID, repository URL, commit SHA, Containerfile digest, build context digest, Buildah image/version digest, base-image digests, start/end state, output manifest digest, SBOM/provenance references, and policy decisions.
- Skopeo performs post-build remote inspection, digest verification, copy/promotion, and air-gap synchronization. Podman is used for disposable runtime smoke tests. ORAS handles generic non-image OCI artifacts.

If users must be mutually untrusted, builds must move to dedicated nodes, microVMs, or an external isolated build service before making that claim.

## Consequences

This supports normal internal build-server use without pretending rootless containers provide tenant isolation. Some Containerfiles requiring privileged operations, host devices, nested runtimes, unrestricted network access, or unsupported build-instruction extensions will be rejected.

Buildah compatibility must be tested against the accepted Containerfile corpus. A filename accepted by Buildah does not guarantee compatibility with every extension another builder may implement.

## Verification

- Rootless, non-privileged Buildah spike on the pinned K3s, Ubuntu, kernel, OCI runtime, and storage-driver baseline
- Positive builds using `Containerfile` and the accepted `Dockerfile` filename alias
- Negative admission tests for host mounts, devices, privilege, capabilities, namespace sharing, floating Git refs, redirects, and disallowed source hosts
- Egress, DNS rebinding, source-secret and registry-token redaction, resource exhaustion, disk/inode exhaustion, timeout, cancellation, and orphan cleanup tests
- Base-image digest pinning, Skopeo remote verification, OCI manifest/media-type checks, multi-architecture policy, SBOM/provenance attachment, and output attribution tests
- Control-plane, Argo Workflow Controller, and K3s restart reconciliation plus Buildah storage cleanup tests

## References

- [Buildah project](https://github.com/containers/buildah)
- [Buildah build command and isolation](https://github.com/containers/buildah/blob/main/docs/buildah-build.1.md)
- [Skopeo project](https://github.com/containers/skopeo)
- [Podman documentation](https://docs.podman.io/)
- [Kubernetes Pod Security Standards](https://kubernetes.io/docs/concepts/security/pod-security-standards/)

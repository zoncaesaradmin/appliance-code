# ADR 0009: Explicit OCI Toolchain

- Status: Accepted with compatibility gates
- Date: 2026-07-03

## Context

Using generic or legacy container-tool terminology obscures which component owns build, execution, image movement, and artifact operations. The appliance needs one explicit, auditable OCI toolchain that works with zot and does not imply an unselected parallel stack.

## Decision

Use these tools and responsibility boundaries:

| Tool | Product responsibility |
| --- | --- |
| Buildah | Build OCI images from `Containerfile` input and publish them to zot |
| Podman | User-facing local runtime, registry client, and disposable runtime smoke tests |
| Skopeo | Remote image inspection, digest verification, copy/promotion, policy enforcement, and air-gap synchronization |
| ORAS | Generic non-image OCI artifact push, pull, attach, discover, and copy |
| zot | OCI image and artifact storage, distribution, search, and gated extensions |
| Helm | OCI chart workflows where chart support is enabled |

A build file literally named `Dockerfile` is accepted because Buildah supports that filename and syntax. The name does not select or imply another build engine, runtime, daemon, API stack, or support contract. Product commands and architecture descriptions use the selected OCI tool names.

K3s uses its embedded runtime internally to run pods. This is an implementation detail of K3s, not an application-facing toolchain choice. No appliance component mounts or calls the node runtime socket.

Additional rules:

- Pin every tool version and distributed image/binary digest in the compatibility manifest. Test them as one versioned set rather than independently asserting compatibility.
- Use one ephemeral `REGISTRY_AUTH_FILE` format supported by Buildah, Podman, and Skopeo. ORAS and Helm credential handling must be mapped from the same short-lived appliance credential without copying it into logs, command arguments, image layers, or durable home directories.
- Prefer password input over stdin or mounted descriptor/file mechanisms. Never place an API token directly in a process argument.
- Define and ship a restrictive `containers-policy.json`, `registries.conf`, short-name policy, and certificate trust bundle. Reject unqualified image names and unapproved registries in production.
- Resolve and record base images by digest. Builds may accept a human-readable tag only if admission resolves it to a digest before execution and records the resolution; production reproducibility policy may require digest-only input.
- Skopeo verifies the remote output digest and media type after every publication or promotion. A successful builder exit alone is not publication evidence.
- ORAS artifacts require explicit artifact/media types, subject/referrer rules, maximum size, and namespace policy. Do not treat opaque files as untyped blobs.
- V1 production support is `linux/amd64`. Multi-architecture manifest creation, foreign architecture execution, and emulation are out of scope until separately gated.
- User-provided build secrets are out of scope for the first build slice. Source and registry credentials are appliance-issued, short-lived, file-mounted, and deleted with the Job.
- Disable Git submodules, Git LFS downloads, floating refs, remote build contexts, and unbounded redirects by default. Add each only through an explicit source-policy extension and tests.
- Local Go development requires none of these tools. All of Buildah/K3s release evidence and the control-plane's own container image build come from the supported Linux build server/CI lane; macOS is not a supported host for any of this tooling (see docs/dev-container.md).

## Compatibility Gates

- Build one representative corpus with Buildah, including multi-stage, rootless ownership, package installation, `COPY`, cache-disabled, and failure cases.
- Authenticate each selected client to zot with the same appliance API-token model and isolated credential-file lifecycle.
- Verify Podman pull/run, Skopeo inspect/copy/sync and policy rejection, Buildah pull/build/push, ORAS referrers/custom media types, and Helm chart push/pull.
- Verify certificate trust, internal registry allowlists, IPv4/IPv6 stance, offline image preload, blocked public registries, short-name rejection, and credential redaction.
- Prove cancellation, timeout, cleanup, disk/inode exhaustion, interrupted publication, retry/idempotency, and digest reconciliation.
- Use pinned Syft for CycloneDX/SPDX SBOM generation and pinned Grype for vulnerability scanning. Their verified binaries/images and signed Grype database bundle must pass the appliance air-gap import policy before Phase 6.
- Use Skopeo's Sigstore-compatible key-based image signing and verification for promoted OCI images. Use pinned Cosign only in release engineering for release-manifest, provenance, and non-image blob signatures; it is not an appliance runtime dependency.

## Consequences

The toolchain is clear and testable, and each adapter can remain narrow. The appliance owns compatibility testing across the selected versions. Adding another builder, runtime, image utility, or artifact client requires an ADR rather than an incidental dependency.

## References

- [Buildah](https://github.com/containers/buildah)
- [Skopeo](https://github.com/containers/skopeo)
- [Podman](https://docs.podman.io/)
- [ORAS](https://oras.land/)
- [OCI image specification](https://github.com/opencontainers/image-spec)
- [OCI distribution specification](https://github.com/opencontainers/distribution-spec)

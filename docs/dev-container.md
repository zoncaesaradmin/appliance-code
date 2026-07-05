# Developer Container

Two Makefile targets (`make dev-shell`, `make dev-run`) give you a
container with a known, shared toolchain (Go, Buildah, Skopeo, etc.).
This is where the control-plane's release container image actually gets
built (`make -C server/backend image`, run from inside `make
dev-shell`), and it's also useful for reproducing a CI build failure
interactively in the exact same environment CI runs in. **It requires a
Linux host with Podman or Docker installed** (the Linux build server
itself, or a Linux dev machine) — see "Supported Hosts" below.

**The control-plane container image is built only on the Linux build
server/CI, inside this shared container — never directly on a developer
machine's host, and never on macOS at all**, regardless of what
container tooling happens to be installed there. Building it straight
on a bare host (even a Linux one) would lose the point of a known,
audited, reproducible toolchain; building it from macOS would additionally
mean no architecture or environment guarantees at all. Day to day, a
laptop only needs `make build`/`make run`/`make test` against the plain
Go binary (see the root README) — no containers, no Podman, nothing
beyond a Go toolchain.

## Supported Hosts

`make dev-shell`/`make dev-run` are **Linux-only** (the build server or
a Linux dev machine). **macOS is not a supported host for any container
tooling in this repo** — do not install Podman/Docker on macOS for this
repo's sake; there is nothing here for it to build or run.

## What This Repository Owns vs. What It Doesn't

- **The dev-container image** (`automation-dev`) is built and published
  from a separate repository, not this one. This repository only
  *consumes* it — pulls a tag, runs it, mounts this repo in. There is no
  Dockerfile for that image here, and there shouldn't be.

## Prerequisites

- A **Linux** host (see "Supported Hosts" above).
- [Podman](https://podman.io/) (the default container engine; see
  `CONTAINER_ENGINE` below to use Docker instead).
- `podman login ghcr.io` first — `ghcr.io/zoncaesaradmin/development-container`
  is **not** public; pulling without logging in fails with `unauthorized`.
  Use a GitHub username and a PAT with `read:packages` scope.

## Usage

```
make dev-shell    # interactive shell in the dev container, this repo mounted at /workspace
make dev-run SCRIPT=path/to/script.sh   # run one script in the dev container, then exit
```

`make dev-shell` is for manual work: reproducing a CI failure, poking
around the toolchain, running ad hoc commands against the mounted tree
in the same environment CI uses. Type `exit` (or
Ctrl-D) to leave — the container is ephemeral (`--rm`), so that's the
entire teardown; there's nothing else to clean up. `vim` is ensured
present on entry (installed on first use via whatever package manager
the image has, if it isn't already there).

`make dev-run SCRIPT=...` is the non-interactive/automation counterpart:
it runs the given script — a path under this repo, since the whole repo
is mounted at `/workspace` and that's the container's working directory
— inside the same container image, then exits. Use this for scripted
reproduction/debugging steps you don't want to babysit interactively.

Both mount the repo read-write at `/workspace` and persist Go's
build/module caches under `$(DEV_CACHE_DIR)` on the host, so repeated
invocations don't re-download modules or recompile from scratch.

## Configuration

Every setting below is a Makefile variable — override per-invocation
(`make dev-shell DEV_IMAGE_TAG=v0.1.0`), export it in your shell, or copy
[`dev-container/env.example`](../dev-container/env.example) to
`dev-container/env` (gitignored) for a persistent per-developer default.

| Variable | Default | Purpose |
| --- | --- | --- |
| `CONTAINER_ENGINE` | `podman` | Container engine binary (`docker` also works for `dev-shell`/`dev-run`). |
| `DEV_REGISTRY` | `ghcr.io/zoncaesaradmin/development-container` | Registry + repo path for the dev-container image. |
| `DEV_IMAGE_NAME` | `automation-dev` | Image name within the registry. |
| `DEV_IMAGE_TAG` | `latest` | Tag to pull. Pin to a specific version (e.g. `v0.1.0`) for reproducibility. |
| `DEV_IMAGE` | `$(DEV_REGISTRY)/$(DEV_IMAGE_NAME):$(DEV_IMAGE_TAG)` | Full image reference; set directly to bypass the three variables above. |
| `DEV_CACHE_DIR` | `$(HOME)/.cache/appliance-code-dev` | Host directory persisting the Go build/module caches across invocations. |
| `DEV_VOLUME_OPTS` | *(empty)* | Suffix appended to every bind-mount flag. Set to `:Z` on SELinux-enforcing hosts (Fedora, RHEL, CentOS) so Podman can relabel the mounted directories. |

## Developer Workstation Setup

### Linux

**One-time setup:**

```bash
# install Podman via your distro's package manager
podman login ghcr.io     # required — the dev-container image isn't public
```

You do *not* need a separate `podman pull` step — the first `make
dev-shell`/`make dev-run` pulls the image automatically if it isn't
already cached locally. Podman talks to the kernel directly on Linux, so
there's no VM/machine step to manage.

### macOS

**Not supported.** Do not install Podman/Docker on macOS for this repo
— `make dev-shell`/`make dev-run` are Linux-only, and the control-plane
image is only ever built on the Linux build server, never here. Use
`make build`/`make run`/`make test` directly against the Go toolchain
instead (see the root README); for anything needing the shared
container toolchain, use a Linux dev machine or the build server itself.

### Windows (untested)

Podman Desktop for Windows manages a WSL2-backed machine. The gap is
`make`/`bash` themselves — run these commands from inside a WSL2 distro
(where both are native) with Podman Desktop's WSL2 integration enabled,
rather than from PowerShell directly.

## Building the Control-Plane Image

On the Linux build server (or a Linux dev machine), this repo alone is
enough — no sibling checkout of `platformkit` is needed. `platformkit`
is a normal versioned `go.mod` dependency
(`github.com/zoncaesaradmin/platformkit`), and `server/backend/vendor/`
already carries its exact pinned source, so the image build never
touches the network:

```bash
git clone <appliance-code-remote> appliance-code
cd appliance-code

podman login ghcr.io   # once, for the dev-container image itself
make dev-shell          # drops into automation-dev, this repo mounted

# now inside the container:
cd server/backend
make image              # builds appliance-control-plane:<version> via Buildah/Podman, from vendor/
exit                     # tears the container down (--rm); the built image stays
                         # in the build server's local container storage
```

`--privileged --device /dev/fuse` on `DEV_RUN` are what let Buildah/Podman
build a nested image from inside this already-containerized shell.

If `platformkit` (or any other dependency) gets bumped, refresh the
vendored tree once — this does need network access and read access to
the private `platformkit` repo, one-time per machine:

```bash
git config --global url."ssh://git@github.com/".insteadOf "https://github.com/"
go env -w 'GOPRIVATE=github.com/zoncaesaradmin/*'
```

Then, whenever a dependency actually changes:

```bash
cd server/backend
make vendor    # go mod tidy && go mod vendor
git add go.mod go.sum vendor
```

From here, the remaining release-engineering steps (SBOM via Syft,
vulnerability scan via Grype, signing via Cosign, `skopeo copy` export
to `control-plane.oci.tar.zst`) are the build server's CI pipeline's job,
not something this repo's Makefile automates — see
[docs/repository-boundary.md](repository-boundary.md) for the full
release-input artifact contract.

## Why Ephemeral (`--rm`) Instead of a Persistent Container

Each `make dev-shell`/`make dev-run` is a fresh container that's removed
on exit, rather than one long-lived container you start/stop separately.
That keeps this down to two targets with no separate lifecycle to manage
or forget about — the tradeoff is that nothing installed into the
container beyond the image and the mounted caches survives past one
invocation, so anything that needs to persist has to live under
`/workspace` or `DEV_CACHE_DIR`.

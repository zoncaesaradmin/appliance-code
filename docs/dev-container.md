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
- `REGISTRY_USER`/`REGISTRY_TOKEN` exported in the environment — a
  GitHub username and a PAT with `read:packages` scope.
  `ghcr.io/zoncaesaradmin/development-container` is **not** public, and
  these two variables are used non-interactively for every registry
  login this repo's Makefile needs (both the regular pull of the
  dev-container image and, on hosts using rootful `SUDO`, the rootful
  login `dev-sudo-setup` performs — see "Building the Control-Plane
  Image" below). Never typed at a prompt, never committed.

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
| `SUDO` | *(empty)* | Set to `sudo` on hosts where `CONTAINER_ENGINE` runs rootless (needed for `make image` to work — see "Building the Control-Plane Image" below). |
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
touches the network.

### Building the image

`make dev-shell`/`make dev-run` bootstrap everything they need on the
first run — nothing to set up by hand beforehand:

```bash
git clone <appliance-code-remote> appliance-code
cd appliance-code

export REGISTRY_USER=<github-username>
export REGISTRY_TOKEN=<PAT with read:packages>
podman login --username "$REGISTRY_USER" --password-stdin ghcr.io <<<"$REGISTRY_TOKEN"   # once, for the dev-container image itself (rootless)
make dev-shell          # first run only: may prompt once for your sudo password — see below

# now inside the container:
cd server/backend
make image                    # builds appliance-control-plane:<version> via `buildah bud`, from vendor/
make image TAG=v0.1.0          # optional: override the tag (defaults to `git describe`, i.e. VERSION)
make push TAG=v0.1.0           # builds (if needed), then tags and pushes to
                                # ghcr.io/zoncaesaradmin/appliance-code/appliance-control-plane:v0.1.0
                                # — needs REGISTRY_TOKEN scoped to write:packages, not just read:packages
exit                     # tears the container down (--rm); the built image stays
                         # in the build server's local container storage
```

`make push` reuses the same `REGISTRY_USER`/`REGISTRY_TOKEN` as
everything else (non-interactive `--password-stdin`, fails fast if
either is unset) and retargets with `REGISTRY`/`IMAGE_OWNER`/
`IMAGE_REPO`/`IMAGE_NAME` (e.g. `make push REGISTRY=registry.zon.local`
for a future internal registry).

`make dev-shell`/`make dev-run` depend on a `dev-sudo-setup` step (see
its comment in the root `Makefile`) that, only the first time it's
needed on a given host, does two things:

1. Writes a NOPASSWD sudoers rule scoped to exactly the `podman` binary
   path (never a blanket sudo grant), validating it with `visudo -c`
   before it takes effect and rolling back if validation fails. Writing
   this needs one interactive `sudo` authentication — unavoidably, since
   nothing can grant itself root the very first time.
2. Runs `sudo podman login` against the dev-container registry using
   `REGISTRY_USER`/`REGISTRY_TOKEN` (`--password-stdin`, never an
   interactive prompt) — rootful podman keeps its own credential store,
   separate from your regular (rootless) `podman login` above. Fails
   fast with a clear message if either variable isn't set — it will
   never sit waiting for typed credentials.

Both checks are idempotent: on every run after the first, they detect
the sudoers rule already exists and skip it (the login call itself
still runs every time, but is a fast, silent no-op if already
authenticated), so no later `make dev-shell`/`dev-run`/`image`
invocation ever prompts for anything, as long as `REGISTRY_USER`/
`REGISTRY_TOKEN` stay exported (e.g. in the shell profile that runs
these commands, or your CI job's secret env vars).

If a host is already rootful, or only ever uses `dev-shell` for plain
interactive debugging and you don't want this bootstrap to touch
`/etc/sudoers.d` at all, set `SUDO=` (empty) — see
`dev-container/env.example`; `dev-sudo-setup` is a no-op whenever `SUDO`
is empty.

### Why rootful?

A rootless outer container only gets a single, fully-consumed
user-namespace mapping. Buildah building the control-plane image inside
that container needs to create *another*, independent mapping (for the
image layers it extracts, which need real multi-UID/GID ownership, e.g.
`/etc/gshadow` owned by `gid 42`) — and the kernel refuses that second
nested mapping no matter which build tool is used (`podman build` and
`buildah bud` both hit the same wall; `buildah bud` is still preferred
over `podman build` once running rootful, since Buildah's chroot
isolation, `BUILDAH_ISOLATION=chroot`, needs no build-time namespace at
all). Rootful `podman run` gives the outer container a real, unrestricted
namespace, so the nested build inside it just works.

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

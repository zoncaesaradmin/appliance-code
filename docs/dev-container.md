# Developer Container

Two Makefile targets (`make dev-shell`, `make dev-run`) give you a
container with a known, shared toolchain (Go, Buildah, Skopeo, etc.) for
interactive debugging and ad hoc reproduction — most commonly,
investigating a CI build failure in the same environment CI actually
runs in. **They require a Linux host with Podman or Docker installed**
(the Linux build server itself, or a Linux dev machine) — see
"Supported Hosts" below.

**The control-plane container image is built only on the Linux build
server/CI.** This repo intentionally has no `make image` target and no
Containerfile for the control-plane image: building it is entirely the
build server's responsibility, never a developer machine's, and never
macOS specifically, regardless of what container tooling happens to be
installed locally. Doing it locally — even inside this shared container
— would make that machine a release-capable machine with none of the
guarantees a build server gives you: a consistent, audited environment;
controlled access to signing keys; and one fixed architecture, rather
than whatever the laptop happens to be. Day to day, a laptop only needs
`make build`/`make run`/`make test` against the plain Go binary (see the
root README) — no containers, no Podman, nothing beyond a Go toolchain.

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
— there is no Containerfile or image-build target here for it to run,
and `make dev-shell`/`make dev-run` are Linux-only. Use `make
build`/`make run`/`make test` directly against the Go toolchain instead
(see the root README); for anything needing the shared container
toolchain, use a Linux dev machine or the build server itself.

### Windows (untested)

Podman Desktop for Windows manages a WSL2-backed machine. The gap is
`make`/`bash` themselves — run these commands from inside a WSL2 distro
(where both are native) with Podman Desktop's WSL2 integration enabled,
rather than from PowerShell directly.

## Why Ephemeral (`--rm`) Instead of a Persistent Container

Each `make dev-shell`/`make dev-run` is a fresh container that's removed
on exit, rather than one long-lived container you start/stop separately.
That keeps this down to two targets with no separate lifecycle to manage
or forget about — the tradeoff is that nothing installed into the
container beyond the image and the mounted caches survives past one
invocation, so anything that needs to persist has to live under
`/workspace` or `DEV_CACHE_DIR`.

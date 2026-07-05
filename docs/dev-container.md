# Developer Container

This repository builds and containerizes the actual product (the
control-plane server and its Helm chart). Two Makefile targets give you a
container with a known, shared toolchain (Go, Buildah, Skopeo, etc.) to
do that build/test/push work in, instead of relying on whatever's on
your host.

This lives here, not in `appliance-release`, because this is where the
product is actually built and containerized — `appliance-release` only
ever *consumes* already-built, signed release artifacts (see
[docs/repository-boundary.md](repository-boundary.md)); it has no build
step that needs this toolchain.

## What This Repository Owns vs. What It Doesn't

- **The dev-container image** (`automation-dev`) is built and published
  from a separate repository, not this one. This repository only
  *consumes* it — pulls a tag, runs it, mounts this repo in. There is no
  Dockerfile for that image here, and there shouldn't be.

## Prerequisites

- [Podman](https://podman.io/) (the default container engine; see
  `CONTAINER_ENGINE` below to use Docker instead).
- `podman login ghcr.io` first — `ghcr.io/zoncaesaradmin/development-container`
  is **not** public; pulling without logging in fails with `unauthorized`.
  Use a GitHub username and a PAT with `read:packages` scope.
- If your host is Apple Silicon (arm64) and the published image is
  amd64-only, Podman pulls and runs it under emulation automatically
  (you'll see a `WARNING: image platform ... does not match` line — this
  is expected, not an error, just slower than a native image would be).

## Usage

```
make dev-shell    # interactive shell in the dev container, this repo mounted at /workspace
make dev-run SCRIPT=path/to/script.sh   # run one script in the dev container, then exit
```

`make dev-shell` is for manual work: build, test, lint, build/push the
control-plane image, poke around the mounted tree. Type `exit` (or
Ctrl-D) to leave — the container is ephemeral (`--rm`), so that's the
entire teardown; there's nothing else to clean up. `vim` is ensured
present on entry (installed on first use via whatever package manager
the image has, if it isn't already there).

`make dev-run SCRIPT=...` is the non-interactive/automation counterpart:
it runs the given script — a path under this repo, since the whole repo
is mounted at `/workspace` and that's the container's working directory
— inside the same container image, then exits. Use this for scripted
build-and-push flows or anything else you want to run without
babysitting an interactive session.

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

### macOS

**One-time setup:**

```bash
brew install podman     # 1. install Podman
podman machine init      # 2. create the Linux VM Podman runs containers in
podman machine start     # 3. start it
podman login ghcr.io     # 4. required — the dev-container image isn't public
```

You do *not* need a separate `podman pull` step — the first `make
dev-shell`/`make dev-run` pulls the image automatically if it isn't
already cached locally.

**Daily workflow:**

```bash
podman machine start     # if not already running (no-op if it is)
make dev-shell           # or: make dev-run SCRIPT=...
```

**End of day:** either leave the Podman machine running, or
`podman machine stop` — stopping is optional.

### Linux

Same as macOS, minus the VM: Podman talks to the kernel directly, so
skip `podman machine init`/`start`/`stop` entirely.

### Windows (untested)

Podman Desktop for Windows manages a WSL2-backed machine the same way
`podman machine` does on macOS. The gap is `make`/`bash` themselves — run
these commands from inside a WSL2 distro (where both are native) with
Podman Desktop's WSL2 integration enabled, rather than from PowerShell
directly.

## Why Ephemeral (`--rm`) Instead of a Persistent Container

Each `make dev-shell`/`make dev-run` is a fresh container that's removed
on exit, rather than one long-lived container you start/stop separately.
That keeps this down to two targets with no separate lifecycle to manage
or forget about — the tradeoff is that nothing installed into the
container beyond the image and the mounted caches survives past one
invocation, so anything that needs to persist has to live under
`/workspace` or `DEV_CACHE_DIR`.

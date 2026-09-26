#!/usr/bin/env bash
# Build the reviewed Open WebUI source tree and export it under the appliance
# OCI contract. The caller owns source acquisition: this script never fetches
# source from the network. Dockerfile base images are prefetched (online from
# Docker Hub, offline from the LAN build-cache refs passed by the caller).
set -euo pipefail

usage() {
  cat <<'EOF'
usage: export-open-webui-image-archive.sh --source-dir DIR --out-file PATH [options]

Builds the exact source revision in services/open-webui/source.lock, applies
the numbered appliance patches, and exports an OCI archive annotated as:
  registry.local/open-webui:bundled

Options:
  --source-dir DIR           Checked-out Open WebUI source (required).
  --out-file PATH            Destination OCI tar (required).
  --reference-out-file PATH  Write registry.local/open-webui@sha256:... here.
  --image-tag TAG            Local temporary tag (default: pinned ref).
  --node-image REF           Frontend build base (default: docker.io/library/node:22-alpine3.20).
  --python-image REF         Backend base (default: docker.io/library/python:3.11-slim-bookworm).
  --uv-image REF             astral uv mount image (default: ghcr.io/astral-sh/uv:0.12.10).
  --run-gate                 Run services/open-webui/tests/gate-smoke.sh after build.

The source must be a clean Git checkout at the exact locked commit. The build
uses USE_SLIM=true and fails if the resulting image does not declare the slim
mode. It does not publish, deploy, or expose the image.
EOF
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
LOCK_FILE="${REPO_ROOT}/services/open-webui/source.lock"
PATCH_DIR="${REPO_ROOT}/services/open-webui/patches"
GATE_SCRIPT="${REPO_ROOT}/services/open-webui/tests/gate-smoke.sh"
# Keep the optional image on the same explicit product architecture contract
# as every other release archive; do not let Buildah silently use the build
# host architecture during a cross-architecture bundle build.
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/oci-pull.sh"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/target-arch.sh"
target_arch_resolve
host_arch_resolve
REWRITE_PY="${SCRIPT_DIR}/open-webui-dockerfile-rewrite.py"

SOURCE_DIR=""
OUT_FILE=""
REFERENCE_OUT_FILE=""
IMAGE_TAG=""
NODE_IMAGE="${OPEN_WEBUI_NODE_IMAGE:-docker.io/library/node:22-alpine3.20}"
PYTHON_IMAGE="${OPEN_WEBUI_PYTHON_IMAGE:-docker.io/library/python:3.11-slim-bookworm}"
UV_IMAGE="${OPEN_WEBUI_UV_IMAGE:-ghcr.io/astral-sh/uv:0.12.10}"
RUN_GATE=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --source-dir) SOURCE_DIR="${2:-}"; shift 2 ;;
    --out-file) OUT_FILE="${2:-}"; shift 2 ;;
    --reference-out-file) REFERENCE_OUT_FILE="${2:-}"; shift 2 ;;
    --image-tag) IMAGE_TAG="${2:-}"; shift 2 ;;
    --node-image) NODE_IMAGE="${2:-}"; shift 2 ;;
    --python-image) PYTHON_IMAGE="${2:-}"; shift 2 ;;
    --uv-image) UV_IMAGE="${2:-}"; shift 2 ;;
    --run-gate) RUN_GATE=1; shift ;;
    --help|-h) usage; exit 0 ;;
    *) echo "export-open-webui-image-archive: unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

[[ -n "${SOURCE_DIR}" && -d "${SOURCE_DIR}/.git" ]] || { echo "export-open-webui-image-archive: --source-dir must be a Git checkout" >&2; exit 2; }
[[ -n "${OUT_FILE}" ]] || { echo "export-open-webui-image-archive: --out-file is required" >&2; exit 2; }
[[ -n "${NODE_IMAGE}" && -n "${PYTHON_IMAGE}" && -n "${UV_IMAGE}" ]] || { echo "export-open-webui-image-archive: --node-image, --python-image, and --uv-image are required" >&2; exit 2; }
for tool in git buildah skopeo podman python3 tar; do
  command -v "${tool}" >/dev/null 2>&1 || { echo "export-open-webui-image-archive: ${tool} is required" >&2; exit 1; }
done

# shellcheck disable=SC1090
source "${LOCK_FILE}"
[[ "${BUILD_MODE:-}" == "USE_SLIM=true" ]] || { echo "export-open-webui-image-archive: source lock must require USE_SLIM=true" >&2; exit 2; }
actual_commit="$(git -C "${SOURCE_DIR}" rev-parse HEAD)"
[[ "${actual_commit}" == "${UPSTREAM_COMMIT}" ]] || { echo "export-open-webui-image-archive: source commit ${actual_commit} does not match locked ${UPSTREAM_COMMIT}" >&2; exit 2; }
git -C "${SOURCE_DIR}" diff --quiet || { echo "export-open-webui-image-archive: source checkout must be clean before appliance patches" >&2; exit 2; }

workdir="$(mktemp -d)"
frontend_extract=""
cleanup() {
  if [[ -n "${frontend_extract}" ]]; then
    podman rm -f "${frontend_extract}" >/dev/null 2>&1 || true
  fi
  rm -rf "${workdir}"
}
trap cleanup EXIT
build_source="${workdir}/source"
git clone --no-hardlinks --no-local "${SOURCE_DIR}" "${build_source}" >/dev/null
git -C "${build_source}" checkout --detach "${UPSTREAM_COMMIT}" >/dev/null
for patch in "${PATCH_DIR}"/*.patch; do
  [[ -f "${patch}" ]] || { echo "export-open-webui-image-archive: missing patch ${patch}" >&2; exit 1; }
  git -C "${build_source}" apply --check "${patch}"
  git -C "${build_source}" apply "${patch}"
done

# Multi-arch packaging (same path for same-arch and cross-arch):
#   1) Build Node frontend on HOST_ARCH (BUILDPLATFORM) into a throwaway image.
#   2) Extract /app artifacts to a directory --build-context (plain files).
#   3) Build Python runtime on TARGET_ARCH, COPY --from=frontend <paths>.
# Never COPY --from=<frontend image ref>: Buildah only resolves that when the
# image platform matches --arch, so cross-arch freezes would fail while
# same-arch accidentally succeeds.
#
# Frontend base follows HOST_ARCH; python/uv follow TARGET_ARCH. Offline
# freezes therefore need both deps/open-webui seeds when HOST_ARCH != TARGET_ARCH.
node_arch="${HOST_ARCH}"
node_local="localhost/open-webui-node:${node_arch}"
python_local="localhost/open-webui-python:${TARGET_ARCH}"
uv_local="localhost/open-webui-uv:${TARGET_ARCH}"
frontend_local="localhost/open-webui-frontend:build"
echo "export-open-webui-image-archive: HOST_ARCH=${HOST_ARCH} TARGET_ARCH=${TARGET_ARCH}" >&2
if ! oci_skopeo_prefetch_docker "${NODE_IMAGE}" "${node_local}" "${node_arch}"; then
  echo "export-open-webui-image-archive: missing node base ${NODE_IMAGE}" >&2
  echo "export-open-webui-image-archive: seed with TARGET_ARCH=${node_arch} make -C deps/open-webui release" >&2
  exit 1
fi
if ! oci_skopeo_prefetch_docker "${PYTHON_IMAGE}" "${python_local}" "${TARGET_ARCH}"; then
  echo "export-open-webui-image-archive: missing python base ${PYTHON_IMAGE}" >&2
  echo "export-open-webui-image-archive: seed with TARGET_ARCH=${TARGET_ARCH} make -C deps/open-webui release" >&2
  exit 1
fi
if ! oci_skopeo_prefetch_docker "${UV_IMAGE}" "${uv_local}" "${TARGET_ARCH}"; then
  echo "export-open-webui-image-archive: missing uv base ${UV_IMAGE}" >&2
  echo "export-open-webui-image-archive: seed with TARGET_ARCH=${TARGET_ARCH} make -C deps/open-webui release" >&2
  exit 1
fi

dockerfile="${build_source}/Dockerfile"
frontend_dockerfile="${build_source}/Dockerfile.frontend"
[[ -f "${dockerfile}" ]] || { echo "export-open-webui-image-archive: missing Dockerfile in source" >&2; exit 1; }
[[ -f "${REWRITE_PY}" ]] || { echo "export-open-webui-image-archive: missing ${REWRITE_PY}" >&2; exit 1; }

python3 "${REWRITE_PY}" \
  "${dockerfile}" \
  "${frontend_dockerfile}" \
  "${node_local}" \
  "${python_local}" \
  "${uv_local}"

IMAGE_TAG="${IMAGE_TAG:-${UPSTREAM_REF#v}-appliance}"
IMAGE_TAG="$(printf '%s' "${IMAGE_TAG}" | sed 's/[^A-Za-z0-9_.-]/-/g')"
local_ref="localhost/open-webui:${IMAGE_TAG}"

echo "export-open-webui-image-archive: building frontend stage on HOST_ARCH=${node_arch}" >&2
buildah bud --arch "${node_arch}" --target build --pull-never --ulimit nofile=65535:65535 \
  --build-arg USE_SLIM=true \
  --build-arg UID=10011 \
  --build-arg GID=10011 \
  -f "${frontend_dockerfile}" \
  --tag "${frontend_local}" "${build_source}"

# Always materialize frontend outputs as a directory context — including when
# HOST_ARCH == TARGET_ARCH — so both arches share one packaging path.
frontend_ctx="${workdir}/frontend-context"
frontend_extract="zon-open-webui-frontend-extract-$$"
mkdir -p "${frontend_ctx}"
echo "export-open-webui-image-archive: extracting frontend /app into build-context for TARGET_ARCH=${TARGET_ARCH}" >&2
podman create --arch "${node_arch}" --name "${frontend_extract}" "${frontend_local}" >/dev/null
# Copy the whole /app tree so COPY --from=frontend paths stay in sync with
# whatever the rewritten Dockerfile requests (build, backend, package.json, …).
podman cp "${frontend_extract}:/app/." "${frontend_ctx}/"
podman rm -f "${frontend_extract}" >/dev/null
frontend_extract=""
[[ -d "${frontend_ctx}/build" && -d "${frontend_ctx}/backend" && -f "${frontend_ctx}/package.json" ]] || {
  echo "export-open-webui-image-archive: frontend extract incomplete under ${frontend_ctx}" >&2
  exit 1
}
buildah rmi "${frontend_local}" >/dev/null 2>&1 || true

echo "export-open-webui-image-archive: building runtime image for TARGET_ARCH=${TARGET_ARCH}" >&2
buildah bud --arch "${TARGET_ARCH}" --pull-never --ulimit nofile=65535:65535 \
  --build-context "frontend=${frontend_ctx}" \
  --build-arg USE_SLIM=true \
  --build-arg UID=10011 \
  --build-arg GID=10011 \
  --label "org.opencontainers.image.source=${UPSTREAM_URL}" \
  --label "org.opencontainers.image.revision=${UPSTREAM_COMMIT}" \
  --label "io.zon.appliance.open-webui.slim=true" \
  --tag "${local_ref}" "${build_source}"

if ! buildah inspect "${local_ref}" | python3 -c 'import json,sys; image=json.load(sys.stdin); env=image.get("Docker",{}).get("config",{}).get("Env",[]) or image.get("OCIv1",{}).get("config",{}).get("Env",[]) or []; labels=image.get("Docker",{}).get("config",{}).get("Labels",{}) or image.get("OCIv1",{}).get("config",{}).get("Labels",{}) or {}; cfg=image.get("Docker",{}).get("config",{}) or image.get("OCIv1",{}).get("config",{}) or {}; assert labels.get("io.zon.appliance.open-webui.slim") == "true"; assert "USE_SLIM_DOCKER=true" in env; user=(cfg.get("User") or "").strip(); assert user in ("10011:10011", "10011"), user' ; then
  echo "export-open-webui-image-archive: built image does not prove USE_SLIM=true and UID/GID 10011" >&2
  exit 1
fi

if [[ "${RUN_GATE}" == "1" ]]; then
  bash "${GATE_SCRIPT}" "${local_ref}"
fi

layout="${workdir}/oci"
skopeo copy --override-os linux --override-arch "${TARGET_ARCH}" "containers-storage:${local_ref}" "oci:${layout}:registry.local/open-webui:bundled"
digest="$(python3 - "${layout}/index.json" <<'PY'
import json, sys
index = json.load(open(sys.argv[1], encoding="utf-8"))
manifests = index.get("manifests", [])
if len(manifests) != 1:
    raise SystemExit("expected exactly one platform manifest")
manifest = manifests[0]
if manifest.get("annotations", {}).get("org.opencontainers.image.ref.name") != "registry.local/open-webui:bundled":
    raise SystemExit("OCI archive missing registry.local/open-webui:bundled annotation")
digest = manifest.get("digest", "")
if not digest.startswith("sha256:") or len(digest) != 71:
    raise SystemExit("invalid platform manifest digest")
print(digest)
PY
)"
mkdir -p "$(dirname "${OUT_FILE}")"
tar -C "${layout}" -cf "${OUT_FILE}" oci-layout index.json blobs
reference="registry.local/open-webui@${digest}"
if [[ -n "${REFERENCE_OUT_FILE}" ]]; then
  mkdir -p "$(dirname "${REFERENCE_OUT_FILE}")"
  printf '%s\n' "${reference}" >"${REFERENCE_OUT_FILE}"
fi
printf 'created Open WebUI OCI archive: %s\nimage reference: %s\n' "${OUT_FILE}" "${reference}"

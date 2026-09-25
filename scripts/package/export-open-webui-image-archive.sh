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
for tool in git buildah skopeo python3 tar; do
  command -v "${tool}" >/dev/null 2>&1 || { echo "export-open-webui-image-archive: ${tool} is required" >&2; exit 1; }
done

# shellcheck disable=SC1090
source "${LOCK_FILE}"
[[ "${BUILD_MODE:-}" == "USE_SLIM=true" ]] || { echo "export-open-webui-image-archive: source lock must require USE_SLIM=true" >&2; exit 2; }
actual_commit="$(git -C "${SOURCE_DIR}" rev-parse HEAD)"
[[ "${actual_commit}" == "${UPSTREAM_COMMIT}" ]] || { echo "export-open-webui-image-archive: source commit ${actual_commit} does not match locked ${UPSTREAM_COMMIT}" >&2; exit 2; }
git -C "${SOURCE_DIR}" diff --quiet || { echo "export-open-webui-image-archive: source checkout must be clean before appliance patches" >&2; exit 2; }

workdir="$(mktemp -d)"
trap 'rm -rf "${workdir}"' EXIT
build_source="${workdir}/source"
git clone --no-hardlinks --no-local "${SOURCE_DIR}" "${build_source}" >/dev/null
git -C "${build_source}" checkout --detach "${UPSTREAM_COMMIT}" >/dev/null
for patch in "${PATCH_DIR}"/*.patch; do
  [[ -f "${patch}" ]] || { echo "export-open-webui-image-archive: missing patch ${patch}" >&2; exit 1; }
  git -C "${build_source}" apply --check "${patch}"
  git -C "${build_source}" apply "${patch}"
done

# Frontend build stage follows BUILDPLATFORM (host); final python stage follows
# TARGET_ARCH. Prefer HOST_ARCH for the node prefetch when set by the caller.
node_arch="${HOST_ARCH:-${TARGET_ARCH}}"
case "${node_arch}" in amd64|arm64) ;; *) node_arch="${TARGET_ARCH}" ;; esac
node_local="localhost/open-webui-node:${node_arch}"
python_local="localhost/open-webui-python:${TARGET_ARCH}"
uv_local="localhost/open-webui-uv:${TARGET_ARCH}"
oci_skopeo_prefetch_docker "${NODE_IMAGE}" "${node_local}" "${node_arch}"
oci_skopeo_prefetch_docker "${PYTHON_IMAGE}" "${python_local}" "${TARGET_ARCH}"
oci_skopeo_prefetch_docker "${UV_IMAGE}" "${uv_local}" "${TARGET_ARCH}"

# Upstream Dockerfile hard-codes registry names. Rewrite to the prefetched
# local refs so --pull-never works offline.
dockerfile="${build_source}/Dockerfile"
[[ -f "${dockerfile}" ]] || { echo "export-open-webui-image-archive: missing Dockerfile in source" >&2; exit 1; }
python3 - "${dockerfile}" "${node_local}" "${python_local}" "${uv_local}" <<'PY'
import pathlib, sys
path = pathlib.Path(sys.argv[1])
node_ref = sys.argv[2]
python_ref = sys.argv[3]
uv_ref = sys.argv[4]
text = path.read_text(encoding="utf-8")
old_node = "FROM --platform=$BUILDPLATFORM node:22-alpine3.20 AS build"
old_python = "FROM python:3.11-slim-bookworm AS base"
old_uv = "RUN --mount=from=ghcr.io/astral-sh/uv:0.12.10,source=/uv,target=/bin/uv"
new_node = f"FROM --platform=$BUILDPLATFORM {node_ref} AS build"
new_python = f"FROM {python_ref} AS base"
new_uv = f"RUN --mount=from={uv_ref},source=/uv,target=/bin/uv"
if old_node not in text:
    raise SystemExit(f"open-webui Dockerfile missing expected node FROM line: {old_node!r}")
if old_python not in text:
    raise SystemExit(f"open-webui Dockerfile missing expected python FROM line: {old_python!r}")
if old_uv not in text:
    raise SystemExit(f"open-webui Dockerfile missing expected uv mount: {old_uv!r}")
path.write_text(
    text.replace(old_node, new_node, 1).replace(old_python, new_python, 1).replace(old_uv, new_uv, 1),
    encoding="utf-8",
)
PY

IMAGE_TAG="${IMAGE_TAG:-${UPSTREAM_REF#v}-appliance}"
IMAGE_TAG="$(printf '%s' "${IMAGE_TAG}" | sed 's/[^A-Za-z0-9_.-]/-/g')"
local_ref="localhost/open-webui:${IMAGE_TAG}"
buildah bud --arch "${TARGET_ARCH}" --pull-never --ulimit nofile=65535:65535 \
  --build-arg USE_SLIM=true \
  --label "org.opencontainers.image.source=${UPSTREAM_URL}" \
  --label "org.opencontainers.image.revision=${UPSTREAM_COMMIT}" \
  --label "io.zon.appliance.open-webui.slim=true" \
  --tag "${local_ref}" "${build_source}"

if ! buildah inspect "${local_ref}" | python3 -c 'import json,sys; image=json.load(sys.stdin); env=image.get("Docker",{}).get("config",{}).get("Env",[]) or image.get("OCIv1",{}).get("config",{}).get("Env",[]) or []; labels=image.get("Docker",{}).get("config",{}).get("Labels",{}) or image.get("OCIv1",{}).get("config",{}).get("Labels",{}) or {}; assert labels.get("io.zon.appliance.open-webui.slim") == "true"; assert "USE_SLIM_DOCKER=true" in env' ; then
  echo "export-open-webui-image-archive: built image does not prove USE_SLIM=true" >&2
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

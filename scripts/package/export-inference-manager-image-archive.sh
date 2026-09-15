#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
usage: export-inference-manager-image-archive.sh --out-file PATH [options]

Builds the thin appliance inference-manager image (no upstream engine layers)
and exports it under the install contract.

Archive annotation: registry.local/inference-manager:bundled
Workload reference: registry.local/inference-manager@sha256:<platform digest>
EOF
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/oci-pull.sh"

OUT_FILE=""
REFERENCE_OUT_FILE=""
IMAGE_TAG=""
RUNTIME_IMAGE="${RUNTIME_IMAGE:-docker.io/library/alpine:3.24.1}"
RUNTIME_PREBAKED="${RUNTIME_PREBAKED:-0}"
GOARCH="${GOARCH:-amd64}"
LOCAL_IMAGE_PREFIX="localhost"
IMAGE_NAME="inference-manager"
PREFETCH_RETRIES=5

retry() {
  local attempt=1
  local max_attempts="$1"
  shift
  while true; do
    if "$@"; then
      return 0
    fi
    if (( attempt >= max_attempts )); then
      return 1
    fi
    sleep $((attempt * 2))
    attempt=$((attempt + 1))
  done
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --out-file) OUT_FILE="${2:-}"; shift 2 ;;
    --reference-out-file) REFERENCE_OUT_FILE="${2:-}"; shift 2 ;;
    --image-tag) IMAGE_TAG="${2:-}"; shift 2 ;;
    --runtime-image) RUNTIME_IMAGE="${2:-}"; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) echo "export-inference-manager-image-archive: unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

if [[ -z "${OUT_FILE}" ]]; then
  echo "export-inference-manager-image-archive: --out-file is required" >&2
  exit 2
fi
for tool in skopeo python3 tar buildah; do
  command -v "${tool}" >/dev/null 2>&1 || {
    echo "export-inference-manager-image-archive: ${tool} is required on PATH" >&2
    exit 1
  }
done

if [[ -z "${IMAGE_TAG}" ]]; then
  IMAGE_TAG="$(git -C "${REPO_ROOT}" describe --tags --always --dirty 2>/dev/null || date -u +%Y%m%d%H%M%S)"
fi
IMAGE_TAG="$(printf '%s' "${IMAGE_TAG}" | sed 's/[^A-Za-z0-9_.-]/-/g')"

mkdir -p "$(dirname "${OUT_FILE}")"
OUT_FILE="$(cd "$(dirname "${OUT_FILE}")" && pwd)/$(basename "${OUT_FILE}")"
LOCAL_BASE="${LOCAL_IMAGE_PREFIX}/alpine-inference-manager-base:3.24.1"
LOCAL_REF="${LOCAL_IMAGE_PREFIX}/${IMAGE_NAME}:${IMAGE_TAG}"

case "${RUNTIME_PREBAKED}" in
  1|true|TRUE|yes|YES|on|ON) ;;
  *)
    retry "${PREFETCH_RETRIES}" oci_skopeo_prefetch_docker "${RUNTIME_IMAGE}" "${LOCAL_BASE}" amd64
    RUNTIME_IMAGE="${LOCAL_BASE}"
    ;;
esac

make -C "${REPO_ROOT}/services/inference-manager" image-local \
  BUILD_ENGINE="buildah bud --pull-never" \
  GOARCH="${GOARCH}" \
  BASE_IMAGE="${RUNTIME_IMAGE}" \
  SERVICE_IMAGE_NAME="${LOCAL_IMAGE_PREFIX}/${IMAGE_NAME}" \
  SERVICE_IMAGE_TAG="${IMAGE_TAG}" \
  SERVICE_IMAGE_BUILD_ARGS="--arch ${GOARCH} --build-arg BASE_IMAGE=${RUNTIME_IMAGE}"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT
LAYOUT="${TMP_DIR}/oci"

skopeo copy --override-os linux --override-arch "${GOARCH}" \
  "containers-storage:${LOCAL_REF}" "oci:${LAYOUT}:registry.local/inference-manager:bundled"

DIGEST="$(python3 - "${LAYOUT}/index.json" <<'PY'
import json, sys
index = json.load(open(sys.argv[1], encoding="utf-8"))
manifests = index.get("manifests", [])
if len(manifests) != 1:
    raise SystemExit(f"expected one platform manifest in OCI index, found {len(manifests)}")
descriptor = manifests[0]
if descriptor.get("annotations", {}).get("org.opencontainers.image.ref.name") != "registry.local/inference-manager:bundled":
    raise SystemExit("OCI archive is missing registry.local/inference-manager:bundled annotation")
digest = descriptor.get("digest", "")
if not digest.startswith("sha256:") or len(digest) != 71:
    raise SystemExit(f"invalid platform manifest digest: {digest!r}")
print(digest)
PY
)"
REFERENCE="registry.local/inference-manager@${DIGEST}"

rm -f "${OUT_FILE}"
tar -C "${LAYOUT}" -cf "${OUT_FILE}" oci-layout index.json blobs
if [[ -n "${REFERENCE_OUT_FILE}" ]]; then
  mkdir -p "$(dirname "${REFERENCE_OUT_FILE}")"
  printf '%s\n' "${REFERENCE}" >"${REFERENCE_OUT_FILE}"
fi

echo "created inference-manager OCI archive: ${OUT_FILE}"
echo "archive annotation: registry.local/inference-manager:bundled"
echo "image reference: ${REFERENCE}"

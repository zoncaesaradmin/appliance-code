#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
usage: export-inference-runtime-image-archive.sh --out-file PATH [options]

Builds or exports the selected inference runtime as an OCI archive under the
appliance install contract.

The archive annotation is registry.local/inference-runtime:bundled and the
emitted workload reference is
registry.local/inference-runtime@sha256:<archive index digest>.

Options:
  --out-file PATH           Output OCI archive tar. Required.
  --reference-out-file PATH Write the canonical digest reference to PATH.
  --source-image REF        Upstream image to re-export. Default:
                            engine-specific pinned image
  --inference-version VER   Compatibility version. Defaults to chart appVersion.
  --engine ENGINE           ollama (default) or vllm.
  --architecture ARCH       amd64 (default); must match the selected package.
EOF
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/oci-pull.sh"
CHART_YAML="${REPO_ROOT}/deploy/charts/appliance-inference/Chart.yaml"
VLLM_VERSION_FILE="${REPO_ROOT}/services/inference-manager/version.env"
OUT_FILE=""
REFERENCE_OUT_FILE=""
SOURCE_IMAGE=""
INFERENCE_VERSION=""
ENGINE="ollama"
ARCHITECTURE="amd64"
LOCAL_IMAGE_PREFIX="localhost"
IMAGE_NAME="appliance-inference-runtime"
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
    --source-image) SOURCE_IMAGE="${2:-}"; shift 2 ;;
    --inference-version) INFERENCE_VERSION="${2:-}"; shift 2 ;;
    --engine) ENGINE="${2:-}"; shift 2 ;;
    --architecture) ARCHITECTURE="${2:-}"; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) echo "export-inference-runtime-image-archive: unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

case "${ENGINE}/${ARCHITECTURE}" in
  ollama/amd64|vllm/amd64|vllm/arm64) ;;
  *) echo "export-inference-runtime-image-archive: unsupported runtime ${ENGINE}/${ARCHITECTURE}" >&2; exit 2 ;;
esac

if [[ -z "${OUT_FILE}" ]]; then
  echo "export-inference-runtime-image-archive: --out-file is required" >&2
  exit 2
fi
for tool in skopeo python3 tar; do
  command -v "${tool}" >/dev/null 2>&1 || {
    echo "export-inference-runtime-image-archive: ${tool} is required on PATH" >&2
    exit 1
  }
done
if ! command -v buildah >/dev/null 2>&1; then
  echo "export-inference-runtime-image-archive: buildah is required for the managed inference image" >&2
  exit 1
fi

if [[ -z "${INFERENCE_VERSION}" && "${ENGINE}" == "ollama" ]]; then
  INFERENCE_VERSION="$(sed -n 's/^appVersion: *"\{0,1\}\([^"[:space:]]*\)"\{0,1\}[[:space:]]*$/\1/p' "${CHART_YAML}")"
fi
if [[ -z "${INFERENCE_VERSION}" && "${ENGINE}" == "vllm" ]]; then
  # shellcheck disable=SC1090
  source "${VLLM_VERSION_FILE}"
  if [[ "${ARCHITECTURE}" == "arm64" ]]; then
    INFERENCE_VERSION="${VLLM_ARM64_VERSION}"
  else
    INFERENCE_VERSION="${VLLM_VERSION}"
  fi
fi
INFERENCE_VERSION="${INFERENCE_VERSION#v}"
if [[ -z "${INFERENCE_VERSION}" ]]; then
  echo "export-inference-runtime-image-archive: unable to derive inference version from ${CHART_YAML}" >&2
  exit 1
fi
IMAGE_TAG="${INFERENCE_VERSION}"
if [[ -z "${SOURCE_IMAGE}" ]]; then
  if [[ "${ENGINE}" == "vllm" ]]; then
    # shellcheck disable=SC1090
    source "${VLLM_VERSION_FILE}"
    if [[ "${ARCHITECTURE}" == "arm64" ]]; then
      SOURCE_IMAGE="${VLLM_ARM64_IMAGE}"
    else
      SOURCE_IMAGE="${VLLM_AMD64_IMAGE}"
    fi
  else
    SOURCE_IMAGE="docker.io/ollama/ollama:${INFERENCE_VERSION}"
  fi
fi

mkdir -p "$(dirname "${OUT_FILE}")"
OUT_FILE="$(cd "$(dirname "${OUT_FILE}")" && pwd)/$(basename "${OUT_FILE}")"
LOCAL_REF="${LOCAL_IMAGE_PREFIX}/${IMAGE_NAME}-${ENGINE}:${IMAGE_TAG}"

# Prefetch the selected Linux architecture into local storage, then re-label under the
# canonical :bundled annotation (same finalize pattern as dns-server export).
retry "${PREFETCH_RETRIES}" \
  oci_skopeo_prefetch_docker "${SOURCE_IMAGE}" "${LOCAL_REF}" "${ARCHITECTURE}"

EXPORT_REF="${LOCAL_REF}"
if [[ "${ENGINE}" == "vllm" || "${ENGINE}" == "ollama" ]]; then
  BUILD_ENGINE_COMMAND="buildah bud --pull-never"
  if [[ "${ARCHITECTURE}" == "arm64" ]]; then
    BUILD_ENGINE_COMMAND="buildah bud --pull-never --arch arm64"
  fi
  make -C "${REPO_ROOT}/services/inference-manager" image-local \
    BUILD_ENGINE="${BUILD_ENGINE_COMMAND}" \
    GOARCH="${ARCHITECTURE}" \
    BASE_IMAGE="${LOCAL_REF}" \
    SERVICE_IMAGE_NAME="${LOCAL_IMAGE_PREFIX}/${IMAGE_NAME}" \
    SERVICE_IMAGE_TAG="${IMAGE_TAG}"
  EXPORT_REF="${LOCAL_IMAGE_PREFIX}/${IMAGE_NAME}:${IMAGE_TAG}"
fi

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT
LAYOUT="${TMP_DIR}/oci"

skopeo copy --override-os linux --override-arch "${ARCHITECTURE}" \
  "containers-storage:${EXPORT_REF}" "oci:${LAYOUT}:registry.local/inference-runtime:bundled"

DIGEST="$(python3 - "${LAYOUT}/index.json" <<'PY'
import json, sys
index = json.load(open(sys.argv[1], encoding="utf-8"))
manifests = index.get("manifests", [])
if len(manifests) != 1:
    raise SystemExit(f"expected one platform manifest in OCI index, found {len(manifests)}")
descriptor = manifests[0]
if descriptor.get("annotations", {}).get("org.opencontainers.image.ref.name") != "registry.local/inference-runtime:bundled":
    raise SystemExit("OCI archive is missing registry.local/inference-runtime:bundled annotation")
digest = descriptor.get("digest", "")
if not digest.startswith("sha256:") or len(digest) != 71:
    raise SystemExit(f"invalid platform manifest digest: {digest!r}")
print(digest)
PY
)"
REFERENCE="registry.local/inference-runtime@${DIGEST}"

rm -f "${OUT_FILE}"
# Pack explicit OCI layout members so the tar has index.json (not ./index.json).
tar -C "${LAYOUT}" -cf "${OUT_FILE}" oci-layout index.json blobs
if [[ -n "${REFERENCE_OUT_FILE}" ]]; then
  mkdir -p "$(dirname "${REFERENCE_OUT_FILE}")"
  printf '%s\n' "${REFERENCE}" >"${REFERENCE_OUT_FILE}"
fi

echo "created inference-runtime OCI archive: ${OUT_FILE}"
echo "source image: ${SOURCE_IMAGE}"
echo "runtime: ${ENGINE}/${ARCHITECTURE}"
echo "archive annotation: registry.local/inference-runtime:bundled"
echo "image reference: ${REFERENCE}"

#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
usage: export-ui-image-archive.sh --out-file PATH [options]

Builds the appliance UI container image into local container storage and
exports it as an OCI archive tarball for release-input packaging.

Options:
  --out-file PATH        Output OCI archive tar path. Required.
  --image-tag VERSION    Local image tag / product version to build/export.
                         Default: CODE_VERSION, PRODUCT_VERSION, VERSION,
                         a reachable git tag, or 0.0.0-dev.
  --image-name NAME      Local image name. Default: appliance-ui.
  --node-image REF       Node build-stage base image. Default: UI_NODE_IMAGE
                         env or Containerfile default.
  --go-image REF         Go build-stage base image. Default: UI_GO_IMAGE env
                         or Containerfile default.
  --runtime-image REF    Runtime base image. Default: UI_RUNTIME_IMAGE env or
                         Containerfile default.
  --help                 Show this help.
EOF
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
UI_DIR="${REPO_ROOT}/services/controlplane-ui"
VERIFY_SCRIPT="${SCRIPT_DIR}/verify-oci-archive-build-metadata.py"

OUT_FILE=""
IMAGE_TAG=""
IMAGE_NAME="appliance-ui"
UI_NODE_IMAGE="${UI_NODE_IMAGE:-}"
UI_GO_IMAGE="${UI_GO_IMAGE:-}"
UI_RUNTIME_IMAGE="${UI_RUNTIME_IMAGE:-}"
LOCAL_IMAGE_PREFIX="localhost"
BUILD_TIME="${BUILD_TIME:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"

sanitize_tag() {
  printf '%s' "$1" | sed 's/[^A-Za-z0-9_.-]/-/g'
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --out-file)
      OUT_FILE="${2:-}"
      shift 2
      ;;
    --image-tag)
      IMAGE_TAG="${2:-}"
      shift 2
      ;;
    --image-name)
      IMAGE_NAME="${2:-}"
      shift 2
      ;;
    --node-image)
      UI_NODE_IMAGE="${2:-}"
      shift 2
      ;;
    --go-image)
      UI_GO_IMAGE="${2:-}"
      shift 2
      ;;
    --runtime-image)
      UI_RUNTIME_IMAGE="${2:-}"
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      echo "export-ui-image-archive: unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "${OUT_FILE}" ]]; then
  echo "export-ui-image-archive: --out-file is required" >&2
  usage >&2
  exit 2
fi

if ! command -v skopeo >/dev/null 2>&1; then
  echo "export-ui-image-archive: skopeo is required on PATH" >&2
  exit 1
fi

if [[ -z "${IMAGE_TAG}" ]]; then
  IMAGE_TAG="${CODE_VERSION:-${PRODUCT_VERSION:-${VERSION:-}}}"
fi
if [[ -z "${IMAGE_TAG}" ]]; then
  IMAGE_TAG="$(git -C "${REPO_ROOT}" describe --tags --dirty 2>/dev/null || true)"
fi
if [[ -z "${IMAGE_TAG}" ]]; then
  IMAGE_TAG="0.0.0-dev"
fi
IMAGE_TAG="$(sanitize_tag "${IMAGE_TAG}")"
COMMIT="$(git -C "${REPO_ROOT}" rev-parse --short HEAD 2>/dev/null || true)"
if [[ -z "${COMMIT}" ]]; then
  echo "export-ui-image-archive: unable to derive commit from repo state" >&2
  exit 1
fi

mkdir -p "$(dirname "${OUT_FILE}")"
OUT_FILE="$(cd "$(dirname "${OUT_FILE}")" && pwd)/$(basename "${OUT_FILE}")"
IMAGE_REF="${LOCAL_IMAGE_PREFIX}/${IMAGE_NAME}:${IMAGE_TAG}"

build_args=()
if [[ -n "${UI_NODE_IMAGE}" ]]; then
  build_args+=(--build-arg "UI_NODE_IMAGE=${UI_NODE_IMAGE}")
fi
if [[ -n "${UI_GO_IMAGE}" ]]; then
  build_args+=(--build-arg "UI_GO_IMAGE=${UI_GO_IMAGE}")
fi
if [[ -n "${UI_RUNTIME_IMAGE}" ]]; then
  build_args+=(--build-arg "UI_RUNTIME_IMAGE=${UI_RUNTIME_IMAGE}")
fi

make -C "${UI_DIR}" image-local \
  SERVICE_IMAGE_NAME="${LOCAL_IMAGE_PREFIX}/${IMAGE_NAME}" \
  SERVICE_IMAGE_TAG="${IMAGE_TAG}" \
  VERSION="${IMAGE_TAG}" \
  COMMIT="${COMMIT}" \
  BUILD_TIME="${BUILD_TIME}" \
  BUILD_NO_CACHE=1 \
  SERVICE_IMAGE_EXTRA_BUILD_ARGS="${build_args[*]}"
rm -f "${OUT_FILE}"
skopeo copy "containers-storage:${IMAGE_REF}" "oci-archive:${OUT_FILE}:${IMAGE_REF}"
python3 "${VERIFY_SCRIPT}" \
  --archive "${OUT_FILE}" \
  --binary-path "appliance-ui" \
  --expect-version "${IMAGE_TAG}" \
  --expect-commit "${COMMIT}" \
  --expect-build-time "${BUILD_TIME}" \
  --label "ui"

echo "created UI image archive:"
echo "  ${OUT_FILE}"
echo "built image tag:"
echo "  ${IMAGE_TAG}"
echo "built commit:"
echo "  ${COMMIT}"
echo "built at:"
echo "  ${BUILD_TIME}"
echo "version source:"
echo "  appliance-code repo state"

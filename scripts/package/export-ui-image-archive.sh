#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
usage: export-ui-image-archive.sh --out-file PATH [options]

Builds the appliance UI container image into local container storage and
exports it as an OCI archive tarball for release-input packaging.

Options:
  --out-file PATH        Output OCI archive tar path. Required.
  --image-tag VERSION    Local image tag to build/export.
                         Default: the appliance-code repo `git describe`
                         version for this checkout.
  --image-name NAME      Local image name. Default: appliance-ui.
  --help                 Show this help.
EOF
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
UI_DIR="${REPO_ROOT}/services/ui"

OUT_FILE=""
IMAGE_TAG=""
IMAGE_NAME="appliance-ui"
LOCAL_IMAGE_PREFIX="localhost"

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
  IMAGE_TAG="$(git -C "${REPO_ROOT}" describe --tags --always --dirty 2>/dev/null || true)"
fi
if [[ -z "${IMAGE_TAG}" ]]; then
  echo "export-ui-image-archive: unable to derive image tag from repo state" >&2
  exit 1
fi
IMAGE_TAG="$(sanitize_tag "${IMAGE_TAG}")"

mkdir -p "$(dirname "${OUT_FILE}")"
OUT_FILE="$(cd "$(dirname "${OUT_FILE}")" && pwd)/$(basename "${OUT_FILE}")"
IMAGE_REF="${LOCAL_IMAGE_PREFIX}/${IMAGE_NAME}:${IMAGE_TAG}"

make -C "${UI_DIR}" image-local IMAGE_NAME="${LOCAL_IMAGE_PREFIX}/${IMAGE_NAME}" IMAGE_TAG="${IMAGE_TAG}"
rm -f "${OUT_FILE}"
skopeo copy "containers-storage:${IMAGE_REF}" "oci-archive:${OUT_FILE}:${IMAGE_REF}"

echo "created UI image archive:"
echo "  ${OUT_FILE}"
echo "built image tag:"
echo "  ${IMAGE_TAG}"
echo "version source:"
echo "  appliance-code repo state"

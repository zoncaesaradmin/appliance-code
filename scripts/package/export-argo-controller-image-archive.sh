#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
usage: export-argo-controller-image-archive.sh --out-file PATH [options]

Builds the appliance-owned Argo workflow-controller wrapper image into
local container storage and exports it as an OCI archive tarball for
release-input packaging.

Options:
  --out-file PATH        Output OCI archive tar path. Required.
  --base-image REF       Upstream workflow-controller image to wrap.
                         Default: quay.io/argoproj/workflow-controller:<chart appVersion>.
  --image-tag VERSION    Local image tag to build/export.
                         Default: the chart appVersion.
  --image-name NAME      Local image name. Default: appliance-argo-controller.
  --help                 Show this help.
EOF
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
SERVICE_DIR="${REPO_ROOT}/services/argo-controller"
CHART_YAML="${REPO_ROOT}/deploy/charts/argo-workflows/Chart.yaml"

OUT_FILE=""
BASE_IMAGE=""
IMAGE_TAG=""
IMAGE_NAME="appliance-argo-controller"
LOCAL_IMAGE_PREFIX="localhost"

derive_argo_version() {
  sed -n 's/^appVersion: *"\{0,1\}\([^"[:space:]]*\)"\{0,1\}[[:space:]]*$/\1/p' "${CHART_YAML}"
}

sanitize_tag() {
  printf '%s' "$1" | sed 's/[^A-Za-z0-9_.-]/-/g'
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --out-file)
      OUT_FILE="${2:-}"
      shift 2
      ;;
    --base-image)
      BASE_IMAGE="${2:-}"
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
      echo "export-argo-controller-image-archive: unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "${OUT_FILE}" ]]; then
  echo "export-argo-controller-image-archive: --out-file is required" >&2
  usage >&2
  exit 2
fi

if ! command -v skopeo >/dev/null 2>&1; then
  echo "export-argo-controller-image-archive: skopeo is required on PATH" >&2
  exit 1
fi

if [[ -z "${IMAGE_TAG}" ]]; then
  IMAGE_TAG="$(derive_argo_version)"
fi
if [[ -z "${IMAGE_TAG}" ]]; then
  echo "export-argo-controller-image-archive: unable to derive image tag from ${CHART_YAML}" >&2
  exit 1
fi
IMAGE_TAG="$(sanitize_tag "${IMAGE_TAG}")"

if [[ -z "${BASE_IMAGE}" ]]; then
  BASE_IMAGE="quay.io/argoproj/workflow-controller:${IMAGE_TAG}"
fi

mkdir -p "$(dirname "${OUT_FILE}")"
OUT_FILE="$(cd "$(dirname "${OUT_FILE}")" && pwd)/$(basename "${OUT_FILE}")"
IMAGE_REF="${LOCAL_IMAGE_PREFIX}/${IMAGE_NAME}:${IMAGE_TAG}"

make -C "${SERVICE_DIR}" image-local IMAGE_NAME="${LOCAL_IMAGE_PREFIX}/${IMAGE_NAME}" IMAGE_TAG="${IMAGE_TAG}" BASE_IMAGE="${BASE_IMAGE}"
rm -f "${OUT_FILE}"
skopeo copy "containers-storage:${IMAGE_REF}" "oci-archive:${OUT_FILE}:${IMAGE_REF}"

echo "created Argo controller image archive:"
echo "  ${OUT_FILE}"
echo "wrapped base image:"
echo "  ${BASE_IMAGE}"
echo "built image ref:"
echo "  ${IMAGE_REF}"

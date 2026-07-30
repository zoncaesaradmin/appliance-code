#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
usage: export-host-agent-image-archive.sh --out-file PATH [options]

Builds the appliance host agent container image into local container storage
and exports it as an OCI archive for release-input packaging.

The archive annotation is registry.local/appliance-host-agent:bundled and the
emitted workload reference is
registry.local/appliance-host-agent@sha256:<archive index digest>.

Options:
  --out-file PATH           Output OCI archive tar path. Required.
  --reference-out-file PATH Write the canonical digest reference to PATH.
  --image-tag VERSION       Local image tag to build/export.
                            Default: the appliance-code repo `git describe`
                            version for this checkout.
  --image-name NAME         Local image name. Default: appliance-host-agent.
    --help                    Show this help.
EOF
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
SERVICE_DIR="${REPO_ROOT}/services/hostagent"

OUT_FILE=""
REFERENCE_OUT_FILE=""
IMAGE_TAG=""
IMAGE_NAME="appliance-host-agent"
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
    --reference-out-file)
      REFERENCE_OUT_FILE="${2:-}"
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
      echo "export-host-agent-image-archive: unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "${OUT_FILE}" ]]; then
  echo "export-host-agent-image-archive: --out-file is required" >&2
  usage >&2
  exit 2
fi

for tool in skopeo python3 tar; do
  command -v "${tool}" >/dev/null 2>&1 || {
    echo "export-host-agent-image-archive: ${tool} is required on PATH" >&2
    exit 1
  }
done

if [[ -z "${IMAGE_TAG}" ]]; then
  IMAGE_TAG="$(git -C "${REPO_ROOT}" describe --tags --always --dirty 2>/dev/null || true)"
fi
if [[ -z "${IMAGE_TAG}" ]]; then
  echo "export-host-agent-image-archive: unable to derive image tag from repo state" >&2
  exit 1
fi
IMAGE_TAG="$(sanitize_tag "${IMAGE_TAG}")"

mkdir -p "$(dirname "${OUT_FILE}")"
OUT_FILE="$(cd "$(dirname "${OUT_FILE}")" && pwd)/$(basename "${OUT_FILE}")"
IMAGE_REF="${LOCAL_IMAGE_PREFIX}/${IMAGE_NAME}:${IMAGE_TAG}"

make -C "${SERVICE_DIR}" build image-local \
  SERVICE_IMAGE_NAME="${LOCAL_IMAGE_PREFIX}/${IMAGE_NAME}" \
  SERVICE_IMAGE_TAG="${IMAGE_TAG}" \
  BUILD_NO_CACHE=1

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT
LAYOUT="${TMP_DIR}/oci"

# Re-export under the canonical :bundled annotation so install
# ValidateOCIArchiveReference / ctr import match the OCI contract.
skopeo copy --override-os linux --override-arch amd64 \
  "containers-storage:${IMAGE_REF}" "oci:${LAYOUT}:registry.local/appliance-host-agent:bundled"

DIGEST="$(python3 - "${LAYOUT}/index.json" <<'PY'
import json, sys
index = json.load(open(sys.argv[1], encoding="utf-8"))
manifests = index.get("manifests", [])
if len(manifests) != 1:
    raise SystemExit(f"expected one platform manifest in OCI index, found {len(manifests)}")
descriptor = manifests[0]
if descriptor.get("annotations", {}).get("org.opencontainers.image.ref.name") != "registry.local/appliance-host-agent:bundled":
    raise SystemExit("OCI archive is missing registry.local/appliance-host-agent:bundled annotation")
digest = descriptor.get("digest", "")
if not digest.startswith("sha256:") or len(digest) != 71:
    raise SystemExit(f"invalid platform manifest digest: {digest!r}")
print(digest)
PY
)"
REFERENCE="registry.local/appliance-host-agent@${DIGEST}"

rm -f "${OUT_FILE}"
# Pack explicit OCI layout members so the tar has index.json (not ./index.json).
tar -C "${LAYOUT}" -cf "${OUT_FILE}" oci-layout index.json blobs

if [[ -n "${REFERENCE_OUT_FILE}" ]]; then
  mkdir -p "$(dirname "${REFERENCE_OUT_FILE}")"
  printf '%s\n' "${REFERENCE}" >"${REFERENCE_OUT_FILE}"
fi

echo "created host agent image archive: ${OUT_FILE}"
echo "archive annotation: registry.local/appliance-host-agent:bundled"
echo "image reference: ${REFERENCE}"

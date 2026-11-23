#!/usr/bin/env bash
set -euo pipefail

usage() { echo 'usage: export-ordering-image-archive.sh --out-file PATH --reference-out-file PATH [--image-tag VERSION]'; }
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
source "${SCRIPT_DIR}/target-arch.sh"
target_arch_resolve
OUT_FILE=""; REFERENCE_OUT_FILE=""; IMAGE_TAG=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --out-file) OUT_FILE="$2"; shift 2;;
    --reference-out-file) REFERENCE_OUT_FILE="$2"; shift 2;;
    --image-tag) IMAGE_TAG="$2"; shift 2;;
    -h|--help) usage; exit 0;;
    *) usage >&2; exit 2;;
  esac
done
[[ -n "${OUT_FILE}" && -n "${REFERENCE_OUT_FILE}" ]] || { usage >&2; exit 2; }
for tool in skopeo python3 tar; do command -v "${tool}" >/dev/null || { echo "${tool} is required" >&2; exit 1; }; done
IMAGE_TAG="${IMAGE_TAG:-${CODE_VERSION:-${PRODUCT_VERSION:-0.0.0-dev}}}"
IMAGE_TAG="$(printf '%s' "${IMAGE_TAG}" | sed 's/[^A-Za-z0-9_.-]/-/g')"
IMAGE_REF="localhost/appliance-ordering:${IMAGE_TAG}"
make -C "${REPO_ROOT}/services/ordering" build image-local \
  SERVICE_IMAGE_NAME="localhost/appliance-ordering" SERVICE_IMAGE_TAG="${IMAGE_TAG}" \
  BUILD_NO_CACHE=1 TARGET_ARCH="${TARGET_ARCH}" GOARCH="${TARGET_ARCH}" \
  GO_IMAGE="${GO_IMAGE:-}" RUNTIME_IMAGE="${RUNTIME_IMAGE:-}" RUNTIME_PREBAKED="${RUNTIME_PREBAKED:-0}"
tmp="$(mktemp -d)"; trap 'rm -rf "${tmp}"' EXIT
skopeo copy --override-os linux --override-arch "${TARGET_ARCH}" \
  "containers-storage:${IMAGE_REF}" "oci:${tmp}/oci:registry.local/ordering:bundled"
digest="$(python3 - "${tmp}/oci/index.json" <<'PY'
import json, sys
data = json.load(open(sys.argv[1], encoding="utf-8"))
manifests = data.get("manifests", [])
if len(manifests) != 1 or manifests[0].get("annotations", {}).get("org.opencontainers.image.ref.name") != "registry.local/ordering:bundled":
    raise SystemExit("ordering archive must contain one bundled manifest")
digest = manifests[0].get("digest", "")
if not digest.startswith("sha256:") or len(digest) != 71: raise SystemExit("invalid image digest")
print(digest)
PY
)"
mkdir -p "$(dirname "${OUT_FILE}")" "$(dirname "${REFERENCE_OUT_FILE}")"
rm -f "${OUT_FILE}"; tar -C "${tmp}/oci" -cf "${OUT_FILE}" oci-layout index.json blobs
printf 'registry.local/ordering@%s\n' "${digest}" >"${REFERENCE_OUT_FILE}"


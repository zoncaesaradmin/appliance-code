#!/usr/bin/env bash
set -euo pipefail

usage() { echo 'usage: export-open-webui-gateway-image-archive.sh --out-file PATH [--reference-out-file PATH]'; }
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
source "${SCRIPT_DIR}/oci-pull.sh"
source "${SCRIPT_DIR}/target-arch.sh"
target_arch_resolve
OUT_FILE=""; REFERENCE_OUT_FILE=""; RUNTIME_IMAGE="${RUNTIME_IMAGE:-docker.io/library/alpine:3.24.1}"
while [[ $# -gt 0 ]]; do case "$1" in --out-file) OUT_FILE="$2"; shift 2;; --reference-out-file) REFERENCE_OUT_FILE="$2"; shift 2;; --runtime-image) RUNTIME_IMAGE="$2"; shift 2;; -h|--help) usage; exit 0;; *) usage >&2; exit 2;; esac; done
[[ -n "${OUT_FILE}" ]] || { usage >&2; exit 2; }
for tool in buildah skopeo python3 tar; do command -v "$tool" >/dev/null || { echo "$tool is required" >&2; exit 1; }; done
tag="$(git -C "${REPO_ROOT}" describe --always --dirty 2>/dev/null || date -u +%Y%m%d%H%M%S)"; tag="${tag//\//-}"
base="localhost/open-webui-gateway-base:${TARGET_ARCH}"; local_ref="localhost/open-webui-gateway:${tag}"
oci_skopeo_prefetch_docker "${RUNTIME_IMAGE}" "${base}" "${TARGET_ARCH}"
make -C "${REPO_ROOT}/services/open-webui-gateway" image-local GOARCH="${TARGET_ARCH}" BASE_IMAGE="${base}" SERVICE_IMAGE_NAME=localhost/open-webui-gateway SERVICE_IMAGE_TAG="${tag}" SERVICE_IMAGE_BUILD_ARGS="--arch ${TARGET_ARCH} --build-arg BASE_IMAGE=${base}"
tmp="$(mktemp -d)"; trap 'rm -rf "${tmp}"' EXIT
skopeo copy --override-os linux --override-arch "${TARGET_ARCH}" "containers-storage:${local_ref}" "oci:${tmp}/oci:registry.local/open-webui-gateway:bundled"
digest="$(python3 - "${tmp}/oci/index.json" <<'PY'
import json,sys
x=json.load(open(sys.argv[1])); m=x['manifests']
assert len(m)==1 and m[0].get('annotations',{}).get('org.opencontainers.image.ref.name')=='registry.local/open-webui-gateway:bundled'
print(m[0]['digest'])
PY
)"
[[ "${digest}" =~ ^sha256:[0-9a-f]{64}$ ]] || { echo 'invalid gateway archive digest' >&2; exit 1; }
mkdir -p "$(dirname "${OUT_FILE}")"; rm -f "${OUT_FILE}"; tar -C "${tmp}/oci" -cf "${OUT_FILE}" oci-layout index.json blobs
if [[ -n "${REFERENCE_OUT_FILE}" ]]; then mkdir -p "$(dirname "${REFERENCE_OUT_FILE}")"; printf 'registry.local/open-webui-gateway@%s\n' "${digest}" >"${REFERENCE_OUT_FILE}"; fi

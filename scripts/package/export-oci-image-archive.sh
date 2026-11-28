#!/usr/bin/env bash
set -euo pipefail

usage() { echo "usage: export-oci-image-archive.sh --repository NAME --source-image REF --out-file PATH [--reference-out-file PATH]"; }
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/oci-pull.sh"
source "${SCRIPT_DIR}/target-arch.sh"
target_arch_resolve
OUT_FILE=""
REFERENCE_OUT_FILE=""
SOURCE_IMAGE=""
REPOSITORY=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --out-file) OUT_FILE="${2:-}"; shift 2 ;;
    --reference-out-file) REFERENCE_OUT_FILE="${2:-}"; shift 2 ;;
    --source-image) SOURCE_IMAGE="${2:-}"; shift 2 ;;
    --repository) REPOSITORY="${2:-}"; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) echo "export-oci-image-archive: unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done
[[ -n "${OUT_FILE}" && -n "${SOURCE_IMAGE}" && -n "${REPOSITORY}" ]] || { echo "export-oci-image-archive: --repository, --source-image, and --out-file are required" >&2; exit 2; }
for tool in skopeo python3 tar; do command -v "${tool}" >/dev/null || { echo "export-oci-image-archive: ${tool} is required" >&2; exit 1; }; done
mkdir -p "$(dirname "${OUT_FILE}")"
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT
local_ref="localhost/appliance-oci-export:${REPOSITORY##*/}"
if ! oci_skopeo_prefetch_docker "${SOURCE_IMAGE}" "${local_ref}" "${TARGET_ARCH}"; then
  echo "export-oci-image-archive: unable to resolve seeded image ${SOURCE_IMAGE}" >&2
  exit 1
fi
skopeo copy --override-os linux --override-arch "${TARGET_ARCH}" "containers-storage:${local_ref}" "oci:${tmp}/oci:registry.local/${REPOSITORY}:bundled"
digest="$(python3 - "${tmp}/oci/index.json" "${TARGET_ARCH}" "${REPOSITORY}" <<'PY'
import json, sys
index_path, want_arch, repository = sys.argv[1:]
root = index_path.rsplit('/', 1)[0]
manifests = json.load(open(index_path, encoding='utf-8')).get('manifests', [])
if len(manifests) != 1:
    raise SystemExit('OCI archive must contain exactly one platform manifest')
descriptor = manifests[0]
want_ref = f'registry.local/{repository}:bundled'
if (descriptor.get('annotations') or {}).get('org.opencontainers.image.ref.name') != want_ref:
    raise SystemExit('OCI archive annotation mismatch')
digest = descriptor.get('digest', '')
if not digest.startswith('sha256:') or len(digest) != 71:
    raise SystemExit('invalid OCI archive digest')
manifest = json.load(open(f"{root}/blobs/sha256/{digest.split(':', 1)[1]}", encoding='utf-8'))
cfg = (manifest.get('config') or {}).get('digest', '')
image_config = json.load(open(f"{root}/blobs/sha256/{cfg.split(':', 1)[1]}", encoding='utf-8'))
if image_config.get('architecture') != want_arch:
    raise SystemExit(f"OCI archive architecture is {image_config.get('architecture')!r}, want {want_arch!r}")
print(digest)
PY
)"
tar -C "${tmp}/oci" -cf "${OUT_FILE}" oci-layout index.json blobs
if [[ -n "${REFERENCE_OUT_FILE}" ]]; then
  mkdir -p "$(dirname "${REFERENCE_OUT_FILE}")"
  printf 'registry.local/%s@%s\n' "${REPOSITORY}" "${digest}" >"${REFERENCE_OUT_FILE}"
fi
echo "created OCI archive for registry.local/${REPOSITORY}: ${OUT_FILE}"

#!/usr/bin/env bash
set -euo pipefail

usage() { echo "usage: export-postgresql-image-archive.sh --out-file PATH [--reference-out-file PATH] [--source-image REF]"; }
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/oci-pull.sh"
source "${SCRIPT_DIR}/target-arch.sh"
target_arch_resolve
OUT_FILE=""
REFERENCE_OUT_FILE=""
SOURCE_IMAGE="${POSTGRES_SOURCE_IMAGE:-docker.io/library/postgres:16.15-alpine3.24}"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --out-file) OUT_FILE="${2:-}"; shift 2 ;;
    --reference-out-file) REFERENCE_OUT_FILE="${2:-}"; shift 2 ;;
    --source-image) SOURCE_IMAGE="${2:-}"; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) echo "export-postgresql-image-archive: unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done
[[ -n "${OUT_FILE}" ]] || { echo "export-postgresql-image-archive: --out-file is required" >&2; exit 2; }
for tool in skopeo python3 tar; do command -v "${tool}" >/dev/null || { echo "export-postgresql-image-archive: ${tool} is required" >&2; exit 1; }; done
mkdir -p "$(dirname "${OUT_FILE}")"
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT
if ! oci_skopeo_prefetch_docker "${SOURCE_IMAGE}" "localhost/appliance-postgres:16.15" "${TARGET_ARCH}"; then
  echo "export-postgresql-image-archive: unable to resolve seeded PostgreSQL image ${SOURCE_IMAGE}" >&2
  exit 1
fi
skopeo copy --override-os linux --override-arch "${TARGET_ARCH}" containers-storage:localhost/appliance-postgres:16.15 "oci:${tmp}/oci:registry.local/postgres:bundled"
digest="$(python3 - "${tmp}/oci/index.json" "${TARGET_ARCH}" <<'PY'
import json, sys
index_path, want_arch = sys.argv[1:]
root = index_path.rsplit('/', 1)[0]
manifests = json.load(open(index_path, encoding='utf-8')).get('manifests', [])
if len(manifests) != 1:
    raise SystemExit('postgres archive must contain exactly one platform manifest')
descriptor = manifests[0]
if (descriptor.get('annotations') or {}).get('org.opencontainers.image.ref.name') != 'registry.local/postgres:bundled':
    raise SystemExit('postgres archive annotation mismatch')
digest = descriptor.get('digest', '')
if not digest.startswith('sha256:') or len(digest) != 71:
    raise SystemExit('invalid postgres archive digest')
manifest = json.load(open(f"{root}/blobs/sha256/{digest.split(':', 1)[1]}", encoding='utf-8'))
cfg = (manifest.get('config') or {}).get('digest', '')
image_config = json.load(open(f"{root}/blobs/sha256/{cfg.split(':', 1)[1]}", encoding='utf-8'))
if image_config.get('architecture') != want_arch:
    raise SystemExit(f"postgres archive architecture is {image_config.get('architecture')!r}, want {want_arch!r}")
print(digest)
PY
)"
tar -C "${tmp}/oci" -cf "${OUT_FILE}" oci-layout index.json blobs
if [[ -n "${REFERENCE_OUT_FILE}" ]]; then
  mkdir -p "$(dirname "${REFERENCE_OUT_FILE}")"
  printf 'registry.local/postgres@%s\n' "${digest}" >"${REFERENCE_OUT_FILE}"
fi
echo "created PostgreSQL OCI archive: ${OUT_FILE}"

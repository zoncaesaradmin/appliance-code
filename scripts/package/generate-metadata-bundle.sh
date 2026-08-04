#!/usr/bin/env bash
# Generate the base appliance metadata-bundle archive for release-input.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SOURCE_DIR="${REPO_ROOT}/metadata-bundle/base"
OUT_DIR="${REPO_ROOT}/.run/metadata-bundle"
SOFTWARE_VERSION="${SOFTWARE_VERSION:-}"
METADATA_REVISION="${METADATA_REVISION:-0}"

usage() {
  cat <<'EOF'
Usage: generate-metadata-bundle.sh [--software-version X.Y.Z] [--metadata-revision N] [--out-dir DIR]

Produces appliance-metadata-bundle-X.Y.Z.N.tar.zst under --out-dir (default .run/metadata-bundle).
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --software-version)
      SOFTWARE_VERSION="$2"
      shift 2
      ;;
    --metadata-revision)
      METADATA_REVISION="$2"
      shift 2
      ;;
    --out-dir)
      OUT_DIR="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "generate-metadata-bundle: unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "${SOFTWARE_VERSION}" ]]; then
  if [[ -f "${REPO_ROOT}/VERSION" ]]; then
    SOFTWARE_VERSION="$(tr -d '[:space:]' <"${REPO_ROOT}/VERSION")"
  else
    SOFTWARE_VERSION="0.0.0"
  fi
fi

# Normalize 0.0.0-dev → 0.0.0
if [[ "${SOFTWARE_VERSION}" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+) ]]; then
  SOFTWARE_VERSION="${BASH_REMATCH[1]}.${BASH_REMATCH[2]}.${BASH_REMATCH[3]}"
else
  echo "generate-metadata-bundle: invalid software version ${SOFTWARE_VERSION}" >&2
  exit 1
fi

METADATA_VERSION="${SOFTWARE_VERSION}.${METADATA_REVISION}"
DIR_NAME="appliance-metadata-bundle-${METADATA_VERSION}"
STAGE="$(mktemp -d)"
trap 'rm -rf "${STAGE}"' EXIT

mkdir -p "${STAGE}/${DIR_NAME}"
cp -a "${SOURCE_DIR}/." "${STAGE}/${DIR_NAME}/"

python3 - "${STAGE}/${DIR_NAME}/bundle.yaml" "${SOFTWARE_VERSION}" "${METADATA_VERSION}" <<'PY'
import sys
from pathlib import Path

path = Path(sys.argv[1])
software, metadata = sys.argv[2], sys.argv[3]
text = path.read_text()
out = []
for line in text.splitlines(True):
    if line.startswith("  softwareVersion:"):
        out.append(f'  softwareVersion: "{software}"\n')
    elif line.startswith("  metadataVersion:"):
        out.append(f'  metadataVersion: "{metadata}"\n')
    else:
        out.append(line)
path.write_text("".join(out))
PY

mkdir -p "${OUT_DIR}"
ARCHIVE="${OUT_DIR}/${DIR_NAME}.tar.zst"
rm -f "${ARCHIVE}"
(
  cd "${STAGE}"
  if command -v zstd >/dev/null 2>&1; then
    tar -cf - "${DIR_NAME}" | zstd -q -f -o "${ARCHIVE}"
  else
    # Air-gapped package hosts often lack zstd and python-zstandard. Use the
    # self-contained pack-tar-zst helper (vendored klauspost/compress, go 1.21)
    # with GOTOOLCHAIN=local so Go never tries to download a newer toolchain.
    PACKER_DIR="${REPO_ROOT}/scripts/package/pack-tar-zst"
    if [[ ! -d "${PACKER_DIR}/vendor" ]]; then
      echo "generate-metadata-bundle: pack-tar-zst vendor tree missing at ${PACKER_DIR}" >&2
      exit 1
    fi
    if ! command -v go >/dev/null 2>&1; then
      echo "generate-metadata-bundle: need zstd CLI or go to write .tar.zst" >&2
      exit 1
    fi
    (
      cd "${PACKER_DIR}"
      GOWORK=off GOTOOLCHAIN=local GOSUMDB=off CGO_ENABLED=0 \
        go run -mod=vendor . \
        -src "${STAGE}/${DIR_NAME}" \
        -out "${ARCHIVE}"
    )
  fi
)

echo "${ARCHIVE}"

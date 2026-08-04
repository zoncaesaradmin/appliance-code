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
(
  cd "${STAGE}"
  if command -v zstd >/dev/null 2>&1; then
    tar -cf - "${DIR_NAME}" | zstd -q -o "${ARCHIVE}"
  else
    python3 - "${DIR_NAME}" "${ARCHIVE}" <<'PY'
import tarfile, sys
from pathlib import Path
try:
    import zstandard as zstd
except ImportError:
    # Fallback: write uncompressed tar then note; prefer klauspost via go tool if needed
    raise SystemExit("zstd CLI or python zstandard package required")
name, archive = sys.argv[1], sys.argv[2]
cctx = zstd.ZstdCompressor()
with open(archive, "wb") as out, cctx.stream_writer(out) as compressor, tarfile.open(fileobj=compressor, mode="w|") as tar:
    tar.add(name)
PY
  fi
)

echo "${ARCHIVE}"

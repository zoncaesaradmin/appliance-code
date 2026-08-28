#!/usr/bin/env bash
# Sync the top-level metadata-bundle payload into the control-plane embed tree.
#
# Authoritative product content lives only at metadata-bundle/base/.
# Go's //go:embed cannot reach outside the controlplane module, so a
# byte-identical snapshot is kept under
# services/controlplane/internal/metadatabundle/embedded/ for binary
# compile-time catalog parsing. Edit metadata-bundle/base only, then run this
# script (or make build / make verify).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
SOURCE_DIR="${REPO_ROOT}/metadata-bundle/base"
DEST_DIR="${REPO_ROOT}/services/controlplane/internal/metadatabundle/embedded"
MODE="sync"

usage() {
  cat <<'EOF'
Usage: sync-embedded-metadata-bundle.sh [--check]

  (default)  Copy metadata-bundle/base → metadatabundle/embedded
  --check    Fail if embedded is out of sync with metadata-bundle/base
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --check)
      MODE="check"
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      echo "sync-embedded-metadata-bundle: unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ ! -d "${SOURCE_DIR}" ]]; then
  echo "sync-embedded-metadata-bundle: source missing: ${SOURCE_DIR}" >&2
  exit 1
fi
if [[ ! -f "${SOURCE_DIR}/bundle.yaml" ]]; then
  echo "sync-embedded-metadata-bundle: expected ${SOURCE_DIR}/bundle.yaml" >&2
  exit 1
fi

# Marker is generated only in the embed tree; never compare it against source.
DIFF_EXCLUDES=(-x README.generated.md -x .DS_Store)

if [[ "${MODE}" == "check" ]]; then
  if [[ ! -d "${DEST_DIR}" ]]; then
    echo "sync-embedded-metadata-bundle: embedded tree missing at ${DEST_DIR}" >&2
    echo "  run: scripts/package/sync-embedded-metadata-bundle.sh" >&2
    exit 1
  fi
  if ! diff -qr "${DIFF_EXCLUDES[@]}" "${SOURCE_DIR}" "${DEST_DIR}" >/dev/null; then
    echo "sync-embedded-metadata-bundle: embedded tree is out of sync with metadata-bundle/base" >&2
    diff -ur "${DIFF_EXCLUDES[@]}" "${SOURCE_DIR}" "${DEST_DIR}" >&2 || true
    echo "  fix: scripts/package/sync-embedded-metadata-bundle.sh && git add services/controlplane/internal/metadatabundle/embedded" >&2
    exit 1
  fi
  echo "sync-embedded-metadata-bundle: ok (embedded matches metadata-bundle/base)"
  exit 0
fi

rm -rf "${DEST_DIR}"
mkdir -p "${DEST_DIR}"
# Prefer rsync when available; fall back to tar to preserve structure without
# dragging AppleDouble / .DS_Store if present via excludes.
if command -v rsync >/dev/null 2>&1; then
  rsync -a --delete \
    --exclude '.DS_Store' \
    --exclude '._*' \
    "${SOURCE_DIR}/" "${DEST_DIR}/"
else
  (
    cd "${SOURCE_DIR}"
    tar -cf - \
      --exclude '.DS_Store' \
      --exclude '._*' \
      .
  ) | (
    cd "${DEST_DIR}"
    tar -xf -
  )
fi

# Marker so operators know not to hand-edit the embed tree.
cat >"${DEST_DIR}/README.generated.md" <<'EOF'
# GENERATED — do not edit

This directory is a snapshot of the product metadata-bundle payload for
`//go:embed` only. **Edit files under `metadata-bundle/base/` at the
repository root**, then run:

```bash
./scripts/package/sync-embedded-metadata-bundle.sh
```

`make verify` fails if this tree drifts from `metadata-bundle/base`.
EOF

echo "sync-embedded-metadata-bundle: synced ${SOURCE_DIR} → ${DEST_DIR}"

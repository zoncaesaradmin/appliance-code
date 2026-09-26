#!/usr/bin/env bash
# Regression: Open WebUI packaging must use one multi-arch path for both
# same-arch and cross-arch freezes (HOST_ARCH frontend + TARGET_ARCH runtime
# via directory --build-context). Never reintroduce COPY --from=<frontend image>.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EXPORT_SH="${ROOT}/scripts/package/export-open-webui-image-archive.sh"
REWRITE_PY="${ROOT}/scripts/package/open-webui-dockerfile-rewrite.py"
ARCH_SH="${ROOT}/scripts/package/target-arch.sh"

fail() {
  echo "test-export-open-webui-multiarch: $*" >&2
  exit 1
}

[[ -f "${EXPORT_SH}" ]] || fail "missing ${EXPORT_SH}"
[[ -f "${REWRITE_PY}" ]] || fail "missing ${REWRITE_PY}"
[[ -f "${ARCH_SH}" ]] || fail "missing ${ARCH_SH}"

grep -q -- '--build-context "frontend=${frontend_ctx}"' "${EXPORT_SH}" \
  || fail "exporter must pass --build-context frontend= (directory context)"
grep -q 'podman cp "${frontend_extract}:/app/." "${frontend_ctx}/"' "${EXPORT_SH}" \
  || fail "exporter must extract frontend /app into the build-context directory"
grep -q 'host_arch_resolve' "${EXPORT_SH}" \
  || fail "exporter must call host_arch_resolve (never fall back HOST:=TARGET)"
grep -q 'node_arch="${HOST_ARCH}"' "${EXPORT_SH}" \
  || fail "frontend arch must be HOST_ARCH only"

# Forbidden: rewriting runtime COPY to an image ref (same-arch-only Buildah path).
if grep -E 'COPY --from=\{frontend_ref\}|from=\{frontend_ref\}|frontend_ref' "${EXPORT_SH}" \
  | grep -v '^[[:space:]]*#' >/dev/null 2>&1; then
  fail "exporter must not rewrite COPY --from= to a frontend image ref"
fi
if grep -n 'COPY --from=.*open-webui-frontend' "${EXPORT_SH}" >/dev/null 2>&1; then
  fail "exporter must not embed COPY --from=open-webui-frontend image refs"
fi

# host_arch_resolve must not silently use TARGET_ARCH.
# shellcheck disable=SC1090
source "${ARCH_SH}"
(
  unset HOST_ARCH || true
  export TARGET_ARCH=arm64
  # With go/uname available this should detect the real host, not TARGET_ARCH.
  host_arch_resolve
  [[ "${HOST_ARCH}" == "amd64" || "${HOST_ARCH}" == "arm64" ]] || fail "host_arch_resolve produced ${HOST_ARCH}"
  # Detected host must match this machine — never the foreign TARGET we set.
  detected="$(go env GOARCH 2>/dev/null || true)"
  [[ -n "${detected}" ]] || detected="$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')"
  [[ "${HOST_ARCH}" == "${detected}" ]] || fail "host_arch_resolve=${HOST_ARCH} != machine ${detected} (must not follow TARGET_ARCH=${TARGET_ARCH})"
)

# Dockerfile rewrite: same output for any HOST/TARGET pair (paths are arch-agnostic).
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT
fixture="${tmp}/Dockerfile"
cat >"${fixture}" <<'EOF'
FROM --platform=$BUILDPLATFORM node:22-alpine3.20 AS build
RUN echo frontend
FROM python:3.11-slim-bookworm AS base
RUN --mount=from=ghcr.io/astral-sh/uv:0.12.10,source=/uv,target=/bin/uv echo uv
COPY --chown=$UID:$GID --from=build /app/build /app/build
COPY --chown=$UID:$GID --from=build /app/CHANGELOG.md /app/CHANGELOG.md
COPY --chown=$UID:$GID --from=build /app/package.json /app/package.json
COPY --from=build /app/backend .
EOF

python3 "${REWRITE_PY}" \
  "${fixture}" \
  "${tmp}/Dockerfile.frontend" \
  "localhost/open-webui-node:amd64" \
  "localhost/open-webui-python:arm64" \
  "localhost/open-webui-uv:arm64"

runtime="$(cat "${fixture}")"
frontend="$(cat "${tmp}/Dockerfile.frontend")"
[[ "${frontend}" == *"FROM localhost/open-webui-node:amd64 AS build"* ]] || fail "frontend Dockerfile missing host node FROM"
[[ "${frontend}" != *"FROM python:"* ]] || fail "frontend Dockerfile must not resolve python stage"
[[ "${runtime}" == *"FROM localhost/open-webui-python:arm64 AS base"* ]] || fail "runtime missing target python FROM"
[[ "${runtime}" == *"COPY --chown=\$UID:\$GID --from=frontend build /app/build"* ]] || fail "runtime missing frontend context COPY for build"
[[ "${runtime}" == *"COPY --from=frontend backend ."* ]] || fail "runtime missing frontend context COPY for backend"
[[ "${runtime}" != *" --from=build "* ]] || fail "runtime still references stage build"
[[ "${runtime}" != *"open-webui-frontend"* ]] || fail "runtime must not COPY --from= frontend image"

# Makefile must export HOST_ARCH into package-open-webui.
# Makefile must export HOST_ARCH into package-open-webui and keep it host-native
# when TARGET_ARCH is foreign (never HOST_ARCH:=TARGET_ARCH).
host_go="$(go env GOARCH)"
case "${host_go}" in
  amd64) foreign_arch=arm64 ;;
  arm64) foreign_arch=amd64 ;;
  *) fail "unsupported go arch ${host_go}" ;;
esac
pkg_out="$(make -n -C "${ROOT}" \
  SUDO= \
  TARGET_ARCH="${foreign_arch}" \
  OPEN_WEBUI_SOURCE_DIR=/tmp/owui-src \
  package-open-webui-image-archive 2>&1)" || true
echo "${pkg_out}" | grep -Eq "HOST_ARCH=\"?${host_go}\"?" \
  || fail "package-open-webui dry-run must keep HOST_ARCH=${host_go} under TARGET_ARCH=${foreign_arch}"
echo "${pkg_out}" | grep -Eq "TARGET_ARCH=\"?${foreign_arch}\"?" \
  || fail "package-open-webui dry-run must set TARGET_ARCH=${foreign_arch}"

echo "test-export-open-webui-multiarch: ok"

#!/usr/bin/env bash
# Shared helpers for packaging scripts that prefetch docker:// images with skopeo.
# Source from scripts/package/*.sh — not a standalone CLI.
#
# Honors DEV_REGISTRY + DEV_REGISTRY_TLS_VERIFY + DEV_REGISTRY_USER/TOKEN when the
# source host is the LAN Artifact Server (offline build-cache pulls).

oci_registry_host() {
  local registry="${1:-${DEV_REGISTRY:-}}"
  registry="$(printf '%s' "${registry}" | tr -d '[:space:]')"
  registry="${registry#https://}"
  registry="${registry#http://}"
  registry="${registry%/}"
  printf '%s' "${registry}"
}

# True when bare image ref (no docker://) is on DEV_REGISTRY.
oci_ref_is_dev_registry() {
  local bare="$1"
  local host
  host="$(oci_registry_host)"
  [[ -n "${host}" ]] || return 1
  [[ "${bare}" == "${host}/"* || "${bare}" == "${host}:"* ]]
}

# skopeo copy docker://BARE -> containers-storage:DEST (linux/amd64).
# LAN Artifact Server pulls get --src-tls-verify=false / --src-creds as needed.
oci_skopeo_prefetch_docker() {
  local bare="$1"
  local dest_storage_ref="$2"
  local -a args=(copy --override-os linux --override-arch amd64)

  if oci_ref_is_dev_registry "${bare}"; then
    case "$(printf '%s' "${DEV_REGISTRY_TLS_VERIFY:-true}" | tr '[:upper:]' '[:lower:]')" in
      0|false|no|off) args+=(--src-tls-verify=false) ;;
    esac
    if [[ -n "${DEV_REGISTRY_USER:-}" && -n "${DEV_REGISTRY_TOKEN:-}" ]]; then
      args+=(--src-creds "${DEV_REGISTRY_USER}:${DEV_REGISTRY_TOKEN}")
    fi
  fi

  skopeo "${args[@]}" "docker://${bare}" "containers-storage:${dest_storage_ref}"
}

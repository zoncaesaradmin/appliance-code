#!/usr/bin/env bash
# Shared hardlink/reflink helpers for packaging large OCI archives without
# rewriting multi-gigabyte blobs on the same filesystem.
# shellcheck shell=bash

# link_or_copy_file SRC DEST
# Prefer hard link, then reflink, then regular copy.
link_or_copy_file() {
  local src="$1"
  local dest="$2"
  if [[ -z "${src}" || -z "${dest}" ]]; then
    echo "link_or_copy_file: SRC and DEST are required" >&2
    return 2
  fi
  if [[ ! -f "${src}" ]]; then
    echo "link_or_copy_file: source file not found: ${src}" >&2
    return 1
  fi
  mkdir -p "$(dirname "${dest}")"
  rm -f "${dest}"
  if ln "${src}" "${dest}" 2>/dev/null; then
    return 0
  fi
  if cp --reflink=auto "${src}" "${dest}" 2>/dev/null; then
    return 0
  fi
  cp -f "${src}" "${dest}"
}

# link_or_copy_tree SRC_DIR DEST_DIR
# Prefer a hardlinked tree (cp -al), then rsync/cp -a.
link_or_copy_tree() {
  local src="$1"
  local dest="$2"
  if [[ -z "${src}" || -z "${dest}" ]]; then
    echo "link_or_copy_tree: SRC_DIR and DEST_DIR are required" >&2
    return 2
  fi
  if [[ ! -d "${src}" ]]; then
    echo "link_or_copy_tree: source directory not found: ${src}" >&2
    return 1
  fi
  rm -rf "${dest}"
  mkdir -p "$(dirname "${dest}")"
  if cp -al "${src}" "${dest}" 2>/dev/null; then
    return 0
  fi
  if command -v rsync >/dev/null 2>&1; then
    mkdir -p "${dest}"
    rsync -a "${src}/" "${dest}/"
    return 0
  fi
  cp -a "${src}" "${dest}"
}

# create_gzip_tarball DEST_ARCHIVE PARENT_DIR ENTRY_NAME
# Prefer pigz when available; fall back to gzip. Same .tar.gz contract either way.
# PACK_GZIP_LEVEL defaults to 1 (fast) for multi-GB pack/export archives; set
# PACK_GZIP_LEVEL=6 for smaller archives when publish bandwidth matters more.
create_gzip_tarball() {
  local dest="$1"
  local parent="$2"
  local entry="$3"
  local level="${PACK_GZIP_LEVEL:-1}"
  if [[ -z "${dest}" || -z "${parent}" || -z "${entry}" ]]; then
    echo "create_gzip_tarball: DEST PARENT ENTRY are required" >&2
    return 2
  fi
  mkdir -p "$(dirname "${dest}")"
  rm -f "${dest}"
  if command -v pigz >/dev/null 2>&1; then
    tar -C "${parent}" -I "pigz -${level}" -cf "${dest}" "${entry}"
    return 0
  fi
  # Portable gzip fallback (GNU/BSD tar). Level control is pigz-only.
  tar -C "${parent}" -czf "${dest}" "${entry}"
}

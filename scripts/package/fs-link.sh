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

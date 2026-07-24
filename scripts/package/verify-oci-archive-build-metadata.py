#!/usr/bin/env python3
"""Fail closed when an OCI archive's service binary embeds stale build metadata."""

from __future__ import annotations

import argparse
import gzip
import io
import json
import tarfile
from pathlib import Path


def load_member_bytes(tf: tarfile.TarFile, name: str) -> bytes:
    member = tf.extractfile(name)
    if member is None:
        raise FileNotFoundError(f"archive member not found: {name}")
    return member.read()


def iter_layer_names(archive_path: Path):
    with tarfile.open(archive_path, "r") as tf:
        index = json.loads(load_member_bytes(tf, "index.json"))
        manifests = index.get("manifests") or []
        if not manifests:
            raise ValueError("index.json has no manifests")
        manifest_digest = manifests[0]["digest"].split(":", 1)[1]
        manifest = json.loads(load_member_bytes(tf, f"blobs/sha256/{manifest_digest}"))
        for layer in manifest.get("layers") or []:
            yield layer["digest"].split(":", 1)[1]


def read_binary_bytes(archive_path: Path, binary_path: str) -> bytes:
    with tarfile.open(archive_path, "r") as tf:
        for layer_digest in iter_layer_names(archive_path):
            layer_bytes = load_member_bytes(tf, f"blobs/sha256/{layer_digest}")
            with tarfile.open(fileobj=gzip.GzipFile(fileobj=io.BytesIO(layer_bytes)), mode="r:") as layer_tf:
                for candidate in (binary_path, f"./{binary_path}"):
                    try:
                        member = layer_tf.extractfile(candidate)
                    except KeyError:
                        member = None
                    if member is not None:
                        return member.read()
    raise FileNotFoundError(f"binary {binary_path!r} not found in {archive_path}")


def require_contains(binary: bytes, expected: str, label: str) -> None:
    needle = expected.encode("utf-8")
    if needle not in binary:
        raise ValueError(f"binary is missing expected {label}: {expected}")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--archive", required=True)
    parser.add_argument("--binary-path", required=True)
    parser.add_argument("--expect-version", required=True)
    parser.add_argument("--expect-commit", required=True)
    parser.add_argument("--expect-build-time", required=True)
    parser.add_argument("--label", default="service")
    args = parser.parse_args()

    archive_path = Path(args.archive)
    binary = read_binary_bytes(archive_path, args.binary_path)
    require_contains(binary, args.expect_version, "version")
    require_contains(binary, args.expect_commit, "commit")
    require_contains(binary, args.expect_build_time, "build time")
    print(
        f"verified {args.label} archive {archive_path} embeds "
        f"version={args.expect_version} commit={args.expect_commit} buildTime={args.expect_build_time}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

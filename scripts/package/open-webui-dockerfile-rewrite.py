#!/usr/bin/env python3
"""Rewrite Open WebUI's Dockerfile for appliance multi-arch packaging.

Always splits the BUILDPLATFORM Node frontend from the TARGET_ARCH Python
runtime and rewrites COPY --from=build /app/<path> to COPY --from=frontend
<path> for a named directory --build-context. Never rewrites to
COPY --from=<image-ref>: that only works when host arch == TARGET_ARCH and
breaks cross-arch freezes.
"""

from __future__ import annotations

import argparse
import pathlib
import re
import sys


OLD_NODE = "FROM --platform=$BUILDPLATFORM node:22-alpine3.20 AS build"
OLD_PYTHON = "FROM python:3.11-slim-bookworm AS base"
OLD_UV = "RUN --mount=from=ghcr.io/astral-sh/uv:0.12.10,source=/uv,target=/bin/uv"


def rewrite(text: str, node_ref: str, python_ref: str, uv_ref: str) -> tuple[str, str]:
    if OLD_NODE not in text:
        raise SystemExit(f"open-webui Dockerfile missing expected node FROM line: {OLD_NODE!r}")
    if OLD_PYTHON not in text:
        raise SystemExit(f"open-webui Dockerfile missing expected python FROM line: {OLD_PYTHON!r}")
    if OLD_UV not in text:
        raise SystemExit(f"open-webui Dockerfile missing expected uv mount: {OLD_UV!r}")

    frontend_text = text.replace(OLD_NODE, f"FROM {node_ref} AS build", 1)
    parts = re.split(r"(?m)^(FROM python:3\.11-slim-bookworm AS base\s*)$", frontend_text, maxsplit=1)
    if len(parts) < 2:
        raise SystemExit("open-webui Dockerfile: could not locate backend FROM for frontend-only split")
    frontend_dockerfile = parts[0]

    final = text.replace(OLD_NODE, f"FROM {node_ref} AS build", 1)
    final = final.replace(OLD_PYTHON, f"FROM {python_ref} AS base", 1)
    final = final.replace(OLD_UV, f"RUN --mount=from={uv_ref},source=/uv,target=/bin/uv", 1)
    final, n = re.subn(
        r"(?ms)^FROM .+ AS build\n.*?^(?=FROM )",
        "",
        final,
        count=1,
    )
    if n != 1:
        raise SystemExit("open-webui Dockerfile: could not remove frontend build stage for final image")

    # Image paths /app/<name> become context-relative <name> for --build-context frontend=.
    final, n = re.subn(
        r"COPY( --chown=\$UID:\$GID)? --from=build /app/(\S+) ",
        r"COPY\1 --from=frontend \2 ",
        final,
    )
    if n < 1 or " --from=build " in final:
        raise SystemExit("open-webui Dockerfile: failed to rewrite COPY --from=build to frontend context")
    if "open-webui-frontend:" in final or "localhost/open-webui-frontend" in final:
        raise SystemExit(
            "open-webui Dockerfile: must not COPY --from= a frontend image ref "
            "(Buildah only resolves same-arch images; use named build-context)"
        )
    return frontend_dockerfile, final


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("dockerfile")
    parser.add_argument("frontend_dockerfile")
    parser.add_argument("node_ref")
    parser.add_argument("python_ref")
    parser.add_argument("uv_ref")
    args = parser.parse_args(argv)

    dockerfile = pathlib.Path(args.dockerfile)
    frontend_df = pathlib.Path(args.frontend_dockerfile)
    text = dockerfile.read_text(encoding="utf-8")
    frontend_text, final_text = rewrite(text, args.node_ref, args.python_ref, args.uv_ref)
    frontend_df.write_text(frontend_text, encoding="utf-8")
    dockerfile.write_text(final_text, encoding="utf-8")
    return 0


if __name__ == "__main__":
    sys.exit(main())

#!/usr/bin/env python3
"""Resolve release packaging artifacts from packages → capabilities catalogs.

Single source of truth: metadata-bundle packages + capabilities catalogs.
Selected APPLIANCE_PACKS determine which capability artifacts must be built
into release-input. No pack-specific ownership is invented here beyond what
those catalogs declare.

PyYAML is preferred when available; otherwise a minimal parser handles the
fixed catalog shapes used by metadata-bundle/base.
"""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

try:
    import yaml
except ImportError:  # pragma: no cover
    yaml = None


KNOWN_ARTIFACTS = (
    "control-plane-image",
    "control-plane-chart",
    "ui-image",
    "blob-storage-image",
    "message-broker-image",
    "message-broker-chart",
    "host-agent-daemon",
    "mdns-host-packages",
    "host-agent-image",
    "artifact-server-image",
    "appliance-registry-chart",
    "coredns-image",
    "appliance-dns-chart",
    "inference-runtime-image",
    "appliance-inference-chart",
    "workspace-provisioner-image",
    "workflows-chart",
    "workflows-crds",
    "workflow-controller-image",
    "workflow-executor-image",
    "jellyfin-image",
)

# Convenience aliases used by build-full-bundle packaging gates.
NEED_ALIASES = {
    "NEED_HOST_AGENT_BINARY": "host-agent-daemon",
    "NEED_HOST_PACKAGES": "mdns-host-packages",
    "NEED_ARTIFACT_SERVER_CHART": "appliance-registry-chart",
    "NEED_DNS_IMAGE": "coredns-image",
    "NEED_DNS_CHART": "appliance-dns-chart",
    "NEED_INFERENCE_CHART": "appliance-inference-chart",
}


def _parse_flow_list(raw: str) -> list[str]:
    raw = raw.strip()
    if not (raw.startswith("[") and raw.endswith("]")):
        return []
    inner = raw[1:-1].strip()
    if not inner:
        return []
    return [part.strip().strip("'\"") for part in inner.split(",") if part.strip()]


def _load_catalog_fallback(text: str, root_key: str) -> dict:
    """Parse packages/capabilities catalogs without PyYAML."""
    root: dict[str, dict] = {}
    section = ""
    current = ""
    in_required = False
    for raw_line in text.splitlines():
        if not raw_line.strip() or raw_line.lstrip().startswith("#"):
            continue
        line = raw_line.rstrip()
        if line == f"{root_key}:":
            section = root_key
            current = ""
            in_required = False
            continue
        if section != root_key:
            continue
        # Top-level entry: "  name:"
        m = re.match(r"^  ([A-Za-z0-9._-]+):\s*$", line)
        if m:
            current = m.group(1)
            root[current] = {}
            in_required = False
            continue
        if not current:
            continue
        # capabilities: [a, b]
        m = re.match(r"^    capabilities:\s*(\[.*\])\s*$", line)
        if m:
            root[current]["capabilities"] = _parse_flow_list(m.group(1))
            continue
        if re.match(r"^    artifacts:\s*$", line):
            root[current].setdefault("artifacts", {})
            in_required = False
            continue
        m = re.match(r"^      required:\s*(\[.*\])\s*$", line)
        if m:
            root[current].setdefault("artifacts", {})["required"] = _parse_flow_list(m.group(1))
            in_required = False
            continue
        if re.match(r"^      required:\s*$", line):
            root[current].setdefault("artifacts", {})["required"] = []
            in_required = True
            continue
        if in_required:
            m = re.match(r"^        -\s+(\S+)\s*$", line)
            if m:
                root[current]["artifacts"]["required"].append(m.group(1))
                continue
            in_required = False
    return root


def load_yaml(path: Path) -> dict:
    text = path.read_text(encoding="utf-8")
    if yaml is not None:
        data = yaml.safe_load(text)
        if not isinstance(data, dict):
            raise SystemExit(f"resolve-pack-artifacts: {path} must contain a mapping")
        return data
    # Infer root key from filename parent ("packages" / "capabilities").
    root_key = path.parent.name
    if root_key not in ("packages", "capabilities"):
        raise SystemExit(
            f"resolve-pack-artifacts: PyYAML missing and cannot infer catalog root for {path}"
        )
    return {root_key: _load_catalog_fallback(text, root_key)}


def resolve_artifacts(packages_path: Path, capabilities_path: Path, packs: list[str]) -> tuple[set[str], set[str]]:
    packages = load_yaml(packages_path).get("packages") or {}
    capabilities = load_yaml(capabilities_path).get("capabilities") or {}
    if not isinstance(packages, dict) or not isinstance(capabilities, dict):
        raise SystemExit("resolve-pack-artifacts: catalogs must define packages/capabilities maps")

    selected_caps: set[str] = set()
    for pack in packs:
        pkg = packages.get(pack)
        if not isinstance(pkg, dict):
            raise SystemExit(f"resolve-pack-artifacts: unknown pack {pack!r} in packages catalog")
        caps = pkg.get("capabilities") or []
        if not isinstance(caps, list):
            raise SystemExit(f"resolve-pack-artifacts: pack {pack!r} capabilities must be a list")
        for cap in caps:
            selected_caps.add(str(cap))

    artifacts: set[str] = set()
    for cap in sorted(selected_caps):
        definition = capabilities.get(cap)
        if not isinstance(definition, dict):
            raise SystemExit(f"resolve-pack-artifacts: unknown capability {cap!r}")
        required = ((definition.get("artifacts") or {}).get("required")) or []
        if not isinstance(required, list):
            raise SystemExit(f"resolve-pack-artifacts: capability {cap!r} artifacts.required must be a list")
        for artifact in required:
            name = str(artifact).strip()
            if not name:
                continue
            if name not in KNOWN_ARTIFACTS:
                raise SystemExit(
                    f"resolve-pack-artifacts: capability {cap!r} references unknown artifact id {name!r}"
                )
            artifacts.add(name)
    return selected_caps, artifacts


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    script_dir = Path(__file__).resolve().parent
    default_root = script_dir.parent.parent / "metadata-bundle" / "base"
    parser.add_argument("--packages", type=Path, default=None)
    parser.add_argument("--capabilities", type=Path, default=None)
    parser.add_argument("--metadata-root", type=Path, default=None)
    parser.add_argument(
        "--packs",
        default="",
        help="Space- or comma-separated pack ids (already resolved, foundation included)",
    )
    parser.add_argument("packs_positional", nargs="*", help="Pack ids (alternative to --packs)")
    parser.add_argument(
        "--format",
        choices=("shell", "list"),
        default="list",
        help="shell: eval-friendly exports; list: one artifact per line",
    )
    parser.add_argument("--shell-exports", action="store_true", help="Alias for --format shell")
    args = parser.parse_args()

    metadata_root = args.metadata_root or default_root
    packages_path = args.packages or (metadata_root / "packages" / "catalog.yaml")
    capabilities_path = args.capabilities or (metadata_root / "capabilities" / "catalog.yaml")
    if args.shell_exports:
        args.format = "shell"

    raw = (args.packs or " ".join(args.packs_positional)).replace(",", " ")
    packs = [token for token in raw.split() if token]
    if not packs:
        raise SystemExit("resolve-pack-artifacts: --packs must not be empty")

    _caps, artifacts = resolve_artifacts(packages_path, capabilities_path, packs)
    ordered = [name for name in KNOWN_ARTIFACTS if name in artifacts]

    if args.format == "list":
        for name in ordered:
            print(name)
        return 0

    # shell format for build-full-bundle / archive-release-input
    print(f"PACK_REQUIRED_ARTIFACTS='{' '.join(ordered)}'")
    print("export PACK_REQUIRED_ARTIFACTS")
    for name in KNOWN_ARTIFACTS:
        flag = "1" if name in artifacts else "0"
        env = "NEED_" + name.upper().replace("-", "_")
        print(f"{env}={flag}")
        print(f"export {env}")
    for alias, artifact in NEED_ALIASES.items():
        flag = "1" if artifact in artifacts else "0"
        print(f"{alias}={flag}")
        print(f"export {alias}")
    return 0


if __name__ == "__main__":
    sys.exit(main())

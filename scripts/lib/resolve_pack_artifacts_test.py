#!/usr/bin/env python3
"""Tests for resolve-pack-artifacts catalog resolution."""

from __future__ import annotations

import subprocess
import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
RESOLVER = ROOT / "scripts" / "lib" / "resolve-pack-artifacts.py"
METADATA = ROOT / "metadata-bundle" / "base"


class ResolvePackArtifactsTest(unittest.TestCase):
    def _resolve(self, *packs: str) -> set[str]:
        out = subprocess.check_output(
            [
                sys.executable,
                str(RESOLVER),
                "--metadata-root",
                str(METADATA),
                *packs,
            ],
            text=True,
        ).strip()
        return set(out.split()) if out else set()

    def test_foundation_excludes_dev_platform_and_deviceuser(self):
        arts = self._resolve("foundation")
        self.assertIn("control-plane-image", arts)
        self.assertIn("ui-image", arts)
        self.assertIn("blob-storage-image", arts)
        self.assertIn("message-broker-image", arts)
        self.assertIn("host-agent-daemon", arts)
        self.assertIn("mdns-host-packages", arts)
        self.assertNotIn("artifact-server-image", arts)
        self.assertNotIn("coredns-image", arts)
        self.assertNotIn("host-agent-image", arts)
        self.assertNotIn("jellyfin-image", arts)
        self.assertNotIn("inference-runtime-image", arts)
        self.assertNotIn("workspace-provisioner-image", arts)

    def test_foundation_plus_acc_llm_arm64(self):
        arts = self._resolve("foundation", "acc-llm-arm64")
        self.assertIn("inference-runtime-image", arts)
        self.assertIn("inference-manager-image", arts)
        self.assertIn("appliance-inference-chart", arts)
        self.assertNotIn("artifact-server-image", arts)
        self.assertNotIn("host-agent-image", arts)

    def test_dev_platform_includes_registry_dns_workflows(self):
        arts = self._resolve("foundation", "dev-platform")
        self.assertIn("artifact-server-image", arts)
        self.assertIn("appliance-registry-chart", arts)
        self.assertIn("coredns-image", arts)
        self.assertIn("appliance-dns-chart", arts)
        self.assertIn("workflows-chart", arts)
        self.assertIn("workspace-provisioner-image", arts)

    def test_deviceuser_includes_host_agent_image(self):
        arts = self._resolve("foundation", "deviceuser")
        self.assertIn("host-agent-image", arts)
        self.assertIn("jellyfin-image", arts)

    def test_shell_exports(self):
        out = subprocess.check_output(
            [
                sys.executable,
                str(RESOLVER),
                "--shell-exports",
                "--metadata-root",
                str(METADATA),
                "foundation",
                "acc-llm-arm64",
            ],
            text=True,
        )
        self.assertIn("NEED_ARTIFACT_SERVER_IMAGE=0", out)
        self.assertIn("NEED_HOST_AGENT_IMAGE=0", out)
        self.assertIn("NEED_HOST_AGENT_BINARY=1", out)
        self.assertIn("NEED_INFERENCE_RUNTIME_IMAGE=1", out)
        self.assertIn("NEED_INFERENCE_MANAGER_IMAGE=1", out)
        self.assertIn("NEED_CONTROL_PLANE_IMAGE=1", out)


if __name__ == "__main__":
    unittest.main()

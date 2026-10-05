"""Contrato Compose local: renderiza configuração, sem daemon/pull/restart."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import unittest

ROOT = Path(__file__).resolve().parents[2]
AGENTS = ROOT / "deploy/agents/linux"
FIXTURE_IMAGE = "sha256:" + "a" * 64


@unittest.skipUnless(shutil.which("docker"), "Docker Compose CLI required; no daemon operation")
class BeylaDiscoveryOverlayTests(unittest.TestCase):
    def render(self, discovery=False, image=None):
        env = {key: os.environ[key] for key in ("PATH", "HOME", "TMPDIR") if key in os.environ}
        if image is not None:
            env["SENTINEL_BEYLA_DISCOVERY_IMAGE"] = image
        command = ["docker", "compose", "--env-file", "/dev/null", "--profile", "beyla",
                   "-f", str(AGENTS / "docker-compose.yml")]
        if discovery:
            command += ["-f", str(AGENTS / "docker-compose.beyla-container-discovery.yml")]
        command += ["config", "--no-env-resolution", "--format", "json"]
        return subprocess.run(command, env=env, text=True, capture_output=True, timeout=30)

    def test_legacy_installer_keeps_legacy_image_and_config(self):
        result = self.render()
        self.assertEqual(result.returncode, 0, "legacy Compose did not render")
        service = json.loads(result.stdout)["services"]["beyla"]
        self.assertTrue(service["image"].startswith("grafana/beyla:3.15.0@sha256:"))
        config = next(v for v in service["volumes"] if v["target"] == "/etc/beyla/config.yml")
        self.assertEqual(Path(config["source"]).name, "beyla.yml")
        self.assertNotIn("DOCKER_HOST", service["environment"])
        legacy = (AGENTS / "beyla.yml").read_text()
        self.assertIn("open_ports:", legacy)
        self.assertNotIn("container_name:", legacy)

    def test_opt_in_requires_distinct_explicit_image(self):
        result = self.render(discovery=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("SENTINEL_BEYLA_DISCOVERY_IMAGE", result.stderr)

    def test_opt_in_overrides_config_and_adds_metadata_without_privilege_changes(self):
        legacy = self.render()
        result = self.render(discovery=True, image=FIXTURE_IMAGE)
        self.assertEqual(legacy.returncode, 0, "legacy Compose did not render")
        self.assertEqual(result.returncode, 0, "opt-in Compose did not render")
        before = json.loads(legacy.stdout)["services"]["beyla"]
        after = json.loads(result.stdout)["services"]["beyla"]
        self.assertEqual(after["image"], FIXTURE_IMAGE)
        self.assertEqual(after["pull_policy"], "never")
        self.assertEqual(after["environment"]["DOCKER_HOST"], "unix:///run/sentinelops-docker-metadata/metadata.sock")
        for key in ("cap_add", "cap_drop", "pid", "network_mode", "read_only", "security_opt", "privileged"):
            self.assertEqual(after.get(key), before.get(key), key)
        configs = [v for v in after["volumes"] if v["target"] == "/etc/beyla/config.yml"]
        self.assertEqual(len(configs), 1)
        self.assertEqual(Path(configs[0]["source"]).name, "beyla-container-discovery.yml")
        self.assertTrue(configs[0]["read_only"])
        metadata = next(v for v in after["volumes"] if v["target"] == "/run/sentinelops-docker-metadata")
        self.assertTrue(metadata["read_only"])
        self.assertEqual(metadata["source"], "/run/sentinelops-docker-metadata")
        self.assertFalse(any("docker.sock" in v.get("source", "") for v in after["volumes"]))


if __name__ == "__main__":
    unittest.main()

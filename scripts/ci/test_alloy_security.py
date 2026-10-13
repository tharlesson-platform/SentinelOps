import importlib.util
import base64
import gzip
import hashlib
import json
from pathlib import Path
import tempfile
import subprocess
import sys
import unittest

spec = importlib.util.spec_from_file_location("gate", Path(__file__).with_name("alloy-security.py"))
gate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(gate)
IMAGE = "sha256:" + "a" * 64


class SecurityGateTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.full = {"Metadata": {"ImageID": IMAGE}, "Results": [{"Vulnerabilities": []}]}
        self.extra = {"Results": [{"Vulnerabilities": [
            {"VulnerabilityID": cve, "PkgName": gate.MOBY,
             "InstalledVersion": gate.VERSION, "Severity": "HIGH"}
            for cve in gate.ABSENT
        ]}]}
        (self.directory / "packages.json").write_text("fixture graph")

    def assess(self, names=None):
        (self.directory / "trivy-full.json").write_text(json.dumps(self.full))
        (self.directory / "trivy-moby.json").write_text(json.dumps(self.extra))
        return gate.assess(self.directory, names or {gate.MOBY + "/client"}, IMAGE, "b" * 64)

    def test_scoped_assessment_keeps_raw_residual(self):
        warning = {"Severity": "UNKNOWN", "VulnerabilityID": "GO-2026-5932"}
        self.full["Results"][0]["Vulnerabilities"].append(warning)
        result = self.assess()
        self.assertEqual(result["residualRawFindings"], [warning])
        self.assertEqual(len(result["moby"]), 4)

    def test_unexpected_sdk_advisory_fails(self):
        self.extra["Results"][0]["Vulnerabilities"][0]["VulnerabilityID"] = "CVE-2099-0001"
        with self.assertRaisesRegex(ValueError, "unreviewed"):
            self.assess()

    def test_vulnerable_daemon_package_fails(self):
        with self.assertRaisesRegex(ValueError, "vulnerable package present"):
            self.assess({gate.MOBY + "/daemon/containerd", gate.MOBY + "/client"})

    def test_vulnerable_plugin_package_fails(self):
        with self.assertRaisesRegex(ValueError, "vulnerable package present"):
            self.assess({gate.MOBY + "/plugin", gate.MOBY + "/client"})

    def test_high_finding_fails_even_without_patch(self):
        self.full["Results"][0]["Vulnerabilities"].append({"Severity": "HIGH"})
        with self.assertRaisesRegex(ValueError, "HIGH/CRITICAL"):
            self.assess()

    def test_image_mismatch_fails(self):
        self.full["Metadata"]["ImageID"] = "sha256:" + "c" * 64
        with self.assertRaisesRegex(ValueError, "image mismatch"):
            self.assess()

    def test_empty_supplement_fails(self):
        self.extra["Results"][0]["Vulnerabilities"] = []
        with self.assertRaisesRegex(ValueError, "unexpectedly empty"):
            self.assess()

    def test_fake_moby_version_fails(self):
        self.extra["Results"][0]["Vulnerabilities"][0]["InstalledVersion"] = "v29.3.1"
        with self.assertRaisesRegex(ValueError, "component mismatch"):
            self.assess()

    def test_stream_rejects_truncated_json(self):
        path = self.directory / "bad.json"
        path.write_text('{"ImportPath":"example"}\n{"ImportPath":')
        with self.assertRaises(ValueError):
            gate.stream(path)

    def test_supplement_truthful_identity(self):
        bom = gate.supplement(IMAGE, "b" * 64)
        self.assertEqual(bom["components"][0]["version"], gate.VERSION)
        self.assertIn("v28.5.2", bom["components"][0]["purl"])

    def fixture_inventory(self):
        binary = self.directory / "alloy"
        binary.write_bytes(b"binary fixture")
        (self.directory / "source-and-binary-sha256.txt").write_text(gate.sha(binary) + "  ./build/alloy\n")
        (self.directory / "moby-base-sha.txt").write_text(gate.BASE)
        (self.directory / "build-info.txt").write_text(
            "alloy: go1.26.9\n\tdep github.com/docker/docker v28.5.2+incompatible\n"
            "\t=> /src/docker (devel)\n\tbuild GOOS=linux\n\tbuild GOARCH=amd64\n\tbuild CGO_ENABLED=1\n")
        env = {"GOOS": "linux", "GOARCH": "amd64", "CGO_ENABLED": "1",
               "GOEXPERIMENT": "", "GOVERSION": "go1.26.9"}
        (self.directory / "go-env.json").write_text(json.dumps(env))
        module = {"Path": gate.MOBY, "Version": gate.VERSION, "Replace": {"Dir": "/src/docker"}}
        self.packages = [
            {"ImportPath": "github.com/grafana/alloy/otel_engine", "Imports": [gate.MOBY + "/client"]},
            {"ImportPath": gate.MOBY + "/client", "DepOnly": True, "Module": module},
        ]
        (self.directory / "modules.json").write_text(json.dumps(module))
        (self.directory / "moby-authz-tests.json").write_text(json.dumps({
            "Action": "pass", "Package": gate.MOBY + "/pkg/authorization"}))
        return binary

    def inspect_fixture(self, binary):
        (self.directory / "packages.json").write_text("\n".join(json.dumps(p) for p in self.packages))
        return gate.inspect(self.directory, binary, IMAGE, Path(gate.__file__).resolve().parents[2] / "deploy/alloy/patches")

    def test_valid_complete_inventory(self):
        binary = self.fixture_inventory()
        digest, names = self.inspect_fixture(binary)
        self.assertEqual(digest, gate.sha(binary))
        self.assertEqual(len(names), 2)

    def test_incomplete_graph_fails(self):
        binary = self.fixture_inventory()
        self.packages[1]["Incomplete"] = True
        with self.assertRaisesRegex(ValueError, "incomplete graph"):
            self.inspect_fixture(binary)

    def test_missing_import_fails(self):
        binary = self.fixture_inventory()
        self.packages.pop()
        with self.assertRaisesRegex(ValueError, "missing imported package"):
            self.inspect_fixture(binary)

    def test_binary_tampering_fails(self):
        binary = self.fixture_inventory()
        binary.write_bytes(b"different")
        with self.assertRaisesRegex(ValueError, "binary hash mismatch"):
            self.inspect_fixture(binary)

    def test_inventory_arch_mismatch_fails(self):
        binary = self.fixture_inventory()
        path = self.directory / "go-env.json"
        path.write_text(path.read_text().replace("amd64", "arm64"))
        with self.assertRaisesRegex(ValueError, "inventory platform mismatch"):
            self.inspect_fixture(binary)

    def test_false_source_version_fails(self):
        binary = self.fixture_inventory()
        self.packages[1]["Module"]["Version"] = "v29.3.1"
        with self.assertRaisesRegex(ValueError, "false Moby"):
            self.inspect_fixture(binary)

    def test_authz_failed_fails(self):
        binary = self.fixture_inventory()
        (self.directory / "moby-authz-tests.json").write_text('{"Action":"fail"}')
        with self.assertRaisesRegex(ValueError, "regression failed"):
            self.inspect_fixture(binary)

    def test_failed_cli_preserves_raw_evidence(self):
        raw = b'{"diagnostic":"report retained on failure"}'
        (self.directory / "trivy-full.json").write_bytes(raw)
        proc = subprocess.run([
            sys.executable, gate.__file__, "assess", str(self.directory),
            str(self.directory / "missing-binary"), IMAGE,
        ], capture_output=True, check=False)
        self.assertNotEqual(proc.returncode, 0)
        bundle = json.loads((self.directory / "image-alloy.json").read_text())
        self.assertIn("error", bundle["assessment"])
        evidence = bundle["rawEvidence"]["trivy-full.json"]
        self.assertEqual(gzip.decompress(base64.b64decode(evidence["data"])), raw)
        self.assertEqual(evidence["sha256"], hashlib.sha256(raw).hexdigest())


if __name__ == "__main__":
    unittest.main()

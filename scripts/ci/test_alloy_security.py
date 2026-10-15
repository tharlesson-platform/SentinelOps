import importlib.util
import base64
import gzip
import hashlib
import json
import io
import copy
import tarfile
from pathlib import Path
import tempfile
import subprocess
import sys
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location("gate", Path(__file__).with_name("alloy-security.py"))
gate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(gate)
upload_spec = importlib.util.spec_from_file_location("upload", Path(__file__).with_name("upload-local-artifact.py"))
upload = importlib.util.module_from_spec(upload_spec)
upload_spec.loader.exec_module(upload)
DIFF_IDS = ["sha256:" + "a" * 64, "sha256:" + "b" * 64]
CONFIG = {"os": "linux", "architecture": "amd64", "config": {"User": "473:473"},
          "rootfs": {"type": "layers", "diff_ids": DIFF_IDS}}
CONFIG_BYTES = json.dumps(CONFIG, sort_keys=True).encode()
IMAGE = gate.content_id(CONFIG_BYTES)


class SecurityGateTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.image = IMAGE
        self.full = {"Metadata": {"ImageID": IMAGE, "DiffIDs": DIFF_IDS,
                                  "Layers": [{"DiffID": diff, "Digest": "sha256:" + str(n) * 64}
                                             for n, diff in enumerate(DIFF_IDS, 1)],
                                  "ImageConfig": copy.deepcopy(CONFIG)}, "Results": [
            {"Class": "os-pkgs", "Type": "ubuntu", "Packages": [{"Name": "libc6"}], "Vulnerabilities": []},
            {"Class": "lang-pkgs", "Type": "gobinary", "Target": "usr/bin/alloy", "Packages": [{"Name": "stdlib"}]},
        ]}
        self.extra = {"Results": [{"Vulnerabilities": [
            {"VulnerabilityID": cve, "PkgName": gate.MOBY,
             "InstalledVersion": gate.VERSION, "Severity": "HIGH"}
            for cve in gate.ABSENT
        ]}]}
        (self.directory / "packages.json").write_text("fixture graph")
        self.identity_fixture()

    def identity_fixture(self, containerd=False):
        self.docker = {"Id": IMAGE, "Os": "linux", "Architecture": "amd64",
                       "RootFS": {"Type": "layers", "Layers": DIFF_IDS}, "Config": CONFIG["config"]}
        if containerd:
            manifest = {"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json",
                        "config": {"mediaType": "application/vnd.oci.image.config.v1+json",
                                   "digest": IMAGE, "size": len(CONFIG_BYTES)},
                        "layers": [{"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
                                    "digest": "sha256:" + str(n) * 64, "size": 10} for n in (1, 2)]}
            self.manifest = json.dumps(manifest, sort_keys=True).encode()
            self.image = gate.content_id(self.manifest)
            self.docker["Id"] = self.image
            self.docker["Descriptor"] = {"mediaType": manifest["mediaType"], "digest": self.image,
                                         "size": len(self.manifest)}
        else:
            self.image = IMAGE
            self.manifest = json.dumps([{"Config": IMAGE[7:] + ".json", "Layers": ["1/layer.tar", "2/layer.tar"]}]).encode()
        (self.directory / "image-config.json").write_bytes(CONFIG_BYTES)
        (self.directory / "image-manifest.json").write_bytes(self.manifest)
        self.write_docker()

    def write_docker(self):
        (self.directory / "image-inspect.json").write_text(json.dumps([self.docker]))

    def identity(self):
        return gate.image_identity(self.directory, self.image, self.full)

    def assess(self, names=None):
        (self.directory / "trivy-full.json").write_text(json.dumps(self.full))
        (self.directory / "trivy-moby.json").write_text(json.dumps(self.extra))
        return gate.assess(self.directory, names or {gate.MOBY + "/client"}, self.image, "b" * 64)

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

    def test_classic_config_binding_passes(self):
        identity = self.identity()
        self.assertEqual(identity["kind"], "docker-classic")
        self.assertEqual(identity["configDigest"], IMAGE)
        self.assertIsNone(identity["manifestDigest"])

    def test_containerd_manifest_config_binding_passes(self):
        self.identity_fixture(containerd=True)
        result = self.assess()
        self.assertNotEqual(self.image, IMAGE)
        self.assertEqual(result["imageIdentity"]["manifestDigest"], self.image)
        self.assertEqual(result["imageIdentity"]["configDigest"], IMAGE)

    def test_containerd_scanner_accepts_only_verified_config_or_manifest_surface(self):
        self.identity_fixture(True)
        for surface, image_id in (("config", IMAGE), ("manifest", self.image)):
            with self.subTest(surface=surface):
                self.full["Metadata"]["ImageID"] = image_id
                identity = self.identity()
                self.assertEqual(identity["trivyImageID"], image_id)
                self.assertEqual(identity["trivyImageIDSurface"], surface)

    def test_both_surfaces_reject_external_ids_config_layers_and_runtime_mismatch(self):
        self.identity_fixture(True)
        original = copy.deepcopy(self.full)
        for surface, image_id in (("config", IMAGE), ("manifest", self.image)):
            for fault in ("external-id", "config", "runtime", "diffids", "layers", "manifest-layers"):
                with self.subTest(surface=surface, fault=fault):
                    self.full = copy.deepcopy(original)
                    metadata = self.full["Metadata"]
                    metadata["ImageID"] = image_id
                    if fault == "external-id":
                        metadata["ImageID"] = "sha256:" + "e" * 64
                    elif fault == "config":
                        metadata["ImageConfig"]["rootfs"]["diff_ids"] = DIFF_IDS[::-1]
                    elif fault == "runtime":
                        metadata["ImageConfig"]["config"]["User"] = "0"
                    elif fault == "diffids":
                        metadata["DiffIDs"] = DIFF_IDS[::-1]
                    elif fault == "layers":
                        metadata["Layers"][0]["DiffID"] = "sha256:" + "e" * 64
                    else:
                        metadata["Layers"][0]["Digest"] = "sha256:" + "e" * 64
                    with self.assertRaises(ValueError):
                        self.identity()

    def test_both_surfaces_reject_tampered_captured_config_and_manifest(self):
        for surface in ("config", "manifest"):
            for member in ("image-config.json", "image-manifest.json"):
                with self.subTest(surface=surface, member=member):
                    self.identity_fixture(True)
                    self.full["Metadata"]["ImageID"] = IMAGE if surface == "config" else self.image
                    target = self.directory / member
                    target.write_bytes(target.read_bytes() + b" ")
                    with self.assertRaises(ValueError):
                        self.identity()

    def test_manifest_surface_cannot_bypass_high_findings(self):
        self.identity_fixture(True)
        self.full["Metadata"]["ImageID"] = self.image
        self.full["Results"][0]["Vulnerabilities"].append({"Severity": "HIGH"})
        with self.assertRaisesRegex(ValueError, "HIGH/CRITICAL"):
            self.assess()

    def test_classic_save_manifest_is_not_a_verified_manifest_id(self):
        self.full["Metadata"]["ImageID"] = gate.content_id(self.manifest)
        with self.assertRaisesRegex(ValueError, "raw scan image mismatch"):
            self.identity()

    def test_containerd_high_gate_still_fails(self):
        self.identity_fixture(containerd=True)
        self.full["Results"][0]["Vulnerabilities"].append({"Severity": "HIGH"})
        with self.assertRaisesRegex(ValueError, "HIGH/CRITICAL"):
            self.assess()

    def test_swapped_config_bytes_fail_for_both_stores(self):
        for containerd in (False, True):
            with self.subTest(containerd=containerd):
                self.identity_fixture(containerd)
                (self.directory / "image-config.json").write_bytes(CONFIG_BYTES + b" ")
                with self.assertRaises(ValueError):
                    self.identity()

    def test_swapped_manifest_bytes_fail(self):
        self.identity_fixture(True)
        (self.directory / "image-manifest.json").write_bytes(self.manifest + b" ")
        with self.assertRaisesRegex(ValueError, "manifest image mismatch"):
            self.identity()

    def test_swapped_docker_image_fails(self):
        self.docker["Id"] = "sha256:" + "c" * 64
        self.write_docker()
        with self.assertRaisesRegex(ValueError, "Docker image mismatch"):
            self.identity()

    def test_swapped_or_reordered_scan_diffids_fail(self):
        for layers in (DIFF_IDS[::-1], DIFF_IDS[:1], ["sha256:" + "d" * 64]):
            with self.subTest(layers=layers):
                self.full["Metadata"]["DiffIDs"] = layers
                with self.assertRaisesRegex(ValueError, "raw scan DiffIDs mismatch"):
                    self.identity()

    def test_swapped_docker_layers_fail(self):
        self.docker["RootFS"] = {"Type": "layers", "Layers": DIFF_IDS[::-1]}
        self.write_docker()
        with self.assertRaisesRegex(ValueError, "Docker DiffIDs mismatch"):
            self.identity()

    def test_swapped_scan_config_fails(self):
        self.full["Metadata"]["ImageConfig"]["config"]["User"] = "0"
        with self.assertRaisesRegex(ValueError, "raw scan config mismatch"):
            self.identity()

    def test_manifest_descriptor_mismatch_fails(self):
        self.identity_fixture(True)
        self.docker["Descriptor"]["size"] += 1
        self.write_docker()
        with self.assertRaisesRegex(ValueError, "descriptor mismatch"):
            self.identity()

    def test_swapped_scanner_compressed_layers_fail(self):
        self.identity_fixture(True)
        self.full["Metadata"]["Layers"][0]["Digest"] = "sha256:" + "f" * 64
        with self.assertRaisesRegex(ValueError, "raw scan manifest layers mismatch"):
            self.identity()

    def test_swapped_scanner_layer_diffids_fail(self):
        self.full["Metadata"]["Layers"][0]["DiffID"] = "sha256:" + "f" * 64
        with self.assertRaisesRegex(ValueError, "raw scan layer identity mismatch"):
            self.identity()

    def test_swapped_manifest_layers_even_same_config_fail(self):
        self.identity_fixture(True)
        manifest = json.loads(self.manifest)
        manifest["layers"].reverse()
        changed = json.dumps(manifest).encode()
        self.image = gate.content_id(changed)
        self.docker["Id"] = self.image
        self.docker["Descriptor"]["digest"] = self.image
        self.docker["Descriptor"]["size"] = len(changed)
        self.write_docker()
        (self.directory / "image-manifest.json").write_bytes(changed)
        with self.assertRaisesRegex(ValueError, "raw scan manifest layers mismatch"):
            self.identity()

    def test_bad_platform_and_missing_scan_metadata_fail(self):
        for key, changed in (("ImageConfig", None), ("Layers", None), ("DiffIDs", [])):
            with self.subTest(key=key):
                old = self.full["Metadata"][key]
                self.full["Metadata"][key] = changed
                with self.assertRaises(ValueError):
                    self.identity()
                self.full["Metadata"][key] = old
        self.docker["Architecture"] = "arm64"
        self.write_docker()
        with self.assertRaisesRegex(ValueError, "image platform mismatch"):
            self.identity()

    def test_manifest_swapped_config_descriptor_fails(self):
        self.identity_fixture(True)
        manifest = json.loads(self.manifest)
        manifest["config"]["digest"] = "sha256:" + "f" * 64
        changed = json.dumps(manifest).encode()
        self.image = gate.content_id(changed)
        self.docker["Id"] = self.image
        self.docker["Descriptor"]["digest"] = self.image
        self.docker["Descriptor"]["size"] = len(changed)
        self.write_docker()
        (self.directory / "image-manifest.json").write_bytes(changed)
        with self.assertRaisesRegex(ValueError, "manifest config mismatch"):
            self.identity()

    def test_index_not_silently_accepted(self):
        self.identity_fixture(True)
        index = json.dumps({"schemaVersion": 2, "mediaType": "application/vnd.oci.image.index.v1+json",
                            "manifests": []}).encode()
        self.image = gate.content_id(index)
        self.docker["Id"] = self.image
        self.write_docker()
        (self.directory / "image-manifest.json").write_bytes(index)
        with self.assertRaisesRegex(ValueError, "unsupported image manifest"):
            self.identity()

    def test_malformed_identity_json_rejected(self):
        for data in (b"{", b'{"x":1,"x":2}', b'{"x":NaN}', b"null", b"[]"):
            with self.subTest(data=data):
                (self.directory / "image-config.json").write_bytes(data)
                with self.assertRaises(ValueError):
                    self.identity()

    def test_missing_identity_evidence_fails(self):
        (self.directory / "image-config.json").unlink()
        with self.assertRaises(OSError):
            self.identity()

    def test_ambiguous_inspect_rejected(self):
        (self.directory / "image-inspect.json").write_text(json.dumps([self.docker, self.docker]))
        with self.assertRaisesRegex(ValueError, "ambiguous"):
            self.identity()

    def save_tar(self, containerd=False, duplicate=False):
        self.identity_fixture(containerd)
        entries = [("blobs/sha256/" + IMAGE[7:] if containerd else IMAGE[7:] + ".json", CONFIG_BYTES)]
        if containerd:
            entries.append(("blobs/sha256/" + self.image[7:], self.manifest))
        else:
            entries.append(("manifest.json", self.manifest))
        if duplicate:
            entries.append(entries[0])
        archive = io.BytesIO()
        with tarfile.open(fileobj=archive, mode="w") as target:
            for name, data in entries:
                info = tarfile.TarInfo(name)
                info.size = len(data)
                target.addfile(info, io.BytesIO(data))
        archive.seek(0)
        (self.directory / "image-config.json").unlink()
        (self.directory / "image-manifest.json").unlink()
        return archive

    def test_stream_capture_classic_and_containerd(self):
        for containerd in (False, True):
            with self.subTest(containerd=containerd):
                archive = self.save_tar(containerd)
                result = gate.capture_identity(self.directory, self.image, archive)
                self.assertEqual(result["configDigest"], IMAGE)
                self.assertEqual(self.identity()["diffIDs"], DIFF_IDS)

    def test_stream_capture_duplicates_fail_before_write(self):
        archive = self.save_tar(True, duplicate=True)
        with self.assertRaisesRegex(ValueError, "duplicate"):
            gate.capture_identity(self.directory, self.image, archive)
        self.assertFalse((self.directory / "image-config.json").exists())

    def test_save_link_rejected_without_extracting(self):
        archive = io.BytesIO()
        with tarfile.open(fileobj=archive, mode="w") as target:
            member = tarfile.TarInfo("manifest.json")
            member.type, member.linkname = tarfile.SYMTYPE, "/outside-fixture"
            target.addfile(member)
        archive.seek(0)
        with self.assertRaisesRegex(ValueError, "linked save member"):
            gate.capture_identity(self.directory, self.image, archive)

    def test_metadata_limit_rejected(self):
        with self.assertRaisesRegex(ValueError, "too large"):
            gate.identity_json(b" " * (1024 * 1024 + 1))

    def test_failed_cli_keeps_raw_config_and_manifest_evidence(self):
        proc = subprocess.run([sys.executable, gate.__file__, "diagnose", str(self.directory),
                               str(self.directory / "missing-binary"), self.image],
                              capture_output=True, timeout=10)
        self.assertNotEqual(proc.returncode, 0)
        packed = json.loads((self.directory / "image-alloy.json").read_text())["rawEvidence"]
        for name in ("image-config.json", "image-manifest.json", "image-inspect.json"):
            original = (self.directory / name).read_bytes()
            self.assertEqual(gzip.decompress(base64.b64decode(packed[name]["data"])), original)
            self.assertEqual(packed[name]["sha256"], hashlib.sha256(original).hexdigest())

    def test_truncated_save_fails(self):
        with self.assertRaises(tarfile.TarError):
            gate.capture_identity(self.directory, self.image, io.BytesIO(b"invalid tar"))

    def test_capture_cli_consumes_stdin_without_docker_or_target_execution(self):
        archive = self.save_tar(True)
        result = subprocess.run([sys.executable, gate.__file__, "capture-identity", str(self.directory),
                                 str(self.directory / "not-executed"), self.image],
                                input=archive.read(), capture_output=True, timeout=10)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(result.stdout)["configDigest"], IMAGE)
        self.identity()

    def test_shell_capture_keeps_immutable_reference_and_scan_policy(self):
        shell = Path(gate.__file__).with_suffix(".sh").read_text()
        self.assertIn('set -euo pipefail', shell)
        self.assertIn('docker image save "$image_id" | python3', shell)
        self.assertLess(shell.index('capture-identity'), shell.index('trivy image --scanners'))
        self.assertIn('--severity UNKNOWN,LOW,MEDIUM,HIGH,CRITICAL --exit-code 0', shell)
        self.assertIn('--ignore-unfixed=false --ignorefile /dev/null', shell)

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
        patcher = mock.patch.object(gate, "binary_metadata", return_value=(self.directory / "build-info.txt").read_text())
        patcher.start()
        self.addCleanup(patcher.stop)
        env = {"GOOS": "linux", "GOARCH": "amd64", "CGO_ENABLED": "1",
               "GOEXPERIMENT": "", "GOVERSION": "go1.26.9"}
        (self.directory / "go-env.json").write_text(json.dumps(env))
        module = {"Path": gate.MOBY, "Version": gate.VERSION, "Replace": {"Dir": "/src/docker"}}
        self.packages = [
            {"ImportPath": "github.com/grafana/alloy/otel_engine", "Imports": [gate.MOBY + "/client"], "Deps": [gate.MOBY + "/client"]},
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
        with self.assertRaisesRegex(ValueError, "Deps coverage mismatch"):
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

    def test_empty_full_scan_fails(self):
        self.full["Results"] = []
        with self.assertRaisesRegex(ValueError, "scan coverage missing"):
            self.assess()

    def test_missing_binary_scan_fails(self):
        self.full["Results"].pop()
        with self.assertRaisesRegex(ValueError, "binary scan coverage missing"):
            self.assess()

    def test_partial_sdk_scan_fails(self):
        self.extra["Results"][0]["Vulnerabilities"].pop()
        with self.assertRaisesRegex(ValueError, "advisory coverage mismatch"):
            self.assess()

    def test_duplicate_sdk_scan_fails(self):
        self.extra["Results"][0]["Vulnerabilities"].append(self.extra["Results"][0]["Vulnerabilities"][0])
        with self.assertRaisesRegex(ValueError, "duplicate supplementary"):
            self.assess()

    def test_real_binary_metadata_mismatch_fails(self):
        binary = self.fixture_inventory()
        with mock.patch.object(gate, "binary_metadata", return_value="different: go1.25.0\n"):
            with self.assertRaisesRegex(ValueError, "embedded binary metadata mismatch"):
                self.inspect_fixture(binary)

    def test_unreachable_package_fails(self):
        binary = self.fixture_inventory()
        self.packages[0]["Imports"] = []
        with self.assertRaisesRegex(ValueError, "unreachable inventory"):
            self.inspect_fixture(binary)

    def test_unrecorded_transitive_dependency_fails(self):
        binary = self.fixture_inventory()
        self.packages[1]["Deps"] = ["missing"]
        with self.assertRaisesRegex(ValueError, "missing transitive dependency"):
            self.inspect_fixture(binary)

    def test_receipt_checks_content_and_identity(self):
        data = b"artifact fixture"
        identity = {"repository": "tharlesson-platform/SentinelOps", "run": 123, "sourceSha": "a" * 40}
        receipt = {"name": "image-alloy.json", "bytes": len(data), "sha256": hashlib.sha256(data).hexdigest(), **identity}
        upload.verify(receipt, data, "image-alloy.json", identity)
        for key in receipt:
            wrong = {**receipt, key: None}
            with self.assertRaisesRegex(ValueError, "receipt mismatch"):
                upload.verify(wrong, data, "image-alloy.json", identity)

    def test_symlink_member_rejected_and_not_packed(self):
        external = self.directory / "external"
        external.write_text("do not include")
        (self.directory / "trivy-full.json").symlink_to(external)
        proc = subprocess.run([
            sys.executable, gate.__file__, "diagnose", str(self.directory),
            str(self.directory / "missing-binary"), "unavailable",
        ], capture_output=True, check=False)
        self.assertNotEqual(proc.returncode, 0)
        bundle = json.loads((self.directory / "image-alloy.json").read_text())
        self.assertNotIn("trivy-full.json", bundle["rawEvidence"])
        self.assertNotIn("external", bundle["rawEvidence"])
        self.assertIn("evidence member", bundle["assessment"]["error"])


if __name__ == "__main__":
    unittest.main()

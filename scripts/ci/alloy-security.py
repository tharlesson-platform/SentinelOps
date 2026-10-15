#!/usr/bin/env python3
"""Inventário complementar e avaliação de alcance; nunca filtra o Trivy bruto."""
import argparse
import base64
import gzip
import hashlib
import json
from pathlib import Path
import re
import sys
import subprocess
import tarfile
import uuid

MOBY = "github.com/docker/docker"
VERSION = "v28.5.2+incompatible"
BASE = "89c5e8fd66634b6128fc4c0e6f1236e2540e46e0"
BACKPORT = "e89edb19ad7de0407a5d31e3111cb01aa10b5a38"
PATCHES = {
    "moby-cve-2026-34040.patch": "2445c60b47a5f43b0b9ed5d25f19ae351fc63ab932a52f460d2ee23d4a893e37",
    "moby-cve-2026-34040-tests.patch": "c6fc52fa701f5c8e5a9770a3f2ec7bad3bfedf799a7b5cfe5fe184a8037c4df6",
}
# Revisão limitada ao código ausente deste artefato, não ao daemon externo.
ABSENT = {
    "CVE-2026-42306": ("daemon", "daemon.openContainerFS"),
    "CVE-2026-41568": ("daemon", "daemon.openContainerFS/fileutils.CreateIfNotExists"),
    "CVE-2026-41567": ("daemon", "daemon.containerExtractToDir/archive.Untar"),
    "CVE-2026-33997": ("plugin", "plugin.validatePrivileges/isEqual"),
}
EVIDENCE_NAMES = {
    "build-info.txt", "go-env.json", "modules.json", "packages.json",
    "moby-base-sha.txt", "moby-authz-tests.json", "moby-authz-platform.json",
    "source-and-binary-sha256.txt", "trivy-full.json", "trivy-moby.json",
    "trivy-sbom.json", "trivy-version.txt", "moby-supplement.cdx.json", "image-inspect.json",
    "image-config.json", "image-manifest.json",
}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def content_id(data):
    return "sha256:" + hashlib.sha256(data).hexdigest()


def valid_id(value):
    return isinstance(value, str) and re.fullmatch(r"sha256:[a-f0-9]{64}", value)


def identity_json(data):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            require(key not in result, "duplicate identity JSON key")
            result[key] = value
        return result
    require(len(data) <= 1024 * 1024, "identity metadata too large")
    return json.loads(data, object_pairs_hook=unique,
                      parse_constant=lambda _: require(False, "invalid identity JSON constant"))


def image_identity(directory, image, raw=None):
    """Bind immutable Docker ID to raw config bytes and ordered uncompressed layers."""
    require(valid_id(image), "invalid local image ID")
    records = identity_json((directory / "image-inspect.json").read_bytes())
    require(isinstance(records, list) and len(records) == 1 and isinstance(records[0], dict),
            "ambiguous image inspect")
    docker = records[0]
    require(docker.get("Id") == image, "Docker image mismatch")
    config_bytes = (directory / "image-config.json").read_bytes()
    config = identity_json(config_bytes)
    require(isinstance(config, dict), "invalid image config")
    config_id = content_id(config_bytes)
    rootfs = config.get("rootfs")
    require(isinstance(rootfs, dict) and rootfs.get("type") == "layers", "invalid config rootfs")
    layers = rootfs.get("diff_ids")
    require(isinstance(layers, list) and layers and all(valid_id(d) for d in layers), "invalid config DiffIDs")
    require(docker.get("RootFS") == {"Type": "layers", "Layers": layers}, "Docker DiffIDs mismatch")
    require(config.get("os") == docker.get("Os") == "linux" and
            config.get("architecture") == docker.get("Architecture") == "amd64", "image platform mismatch")
    runtime = config.get("config")
    require(isinstance(runtime, dict) and isinstance(docker.get("Config"), dict) and
            all(docker["Config"].get(k) == v for k, v in runtime.items()), "Docker runtime config mismatch")
    manifest_bytes = (directory / "image-manifest.json").read_bytes()
    manifest = identity_json(manifest_bytes)
    if image == config_id:
        # Classic Docker: .Id is already the config's content address.
        require(isinstance(manifest, list) and len(manifest) == 1 and isinstance(manifest[0], dict),
                "invalid classic manifest")
        entry = manifest[0]
        require(entry.get("Config") in (config_id[7:] + ".json", "blobs/sha256/" + config_id[7:]),
                "classic config mismatch")
        saved_layers = entry.get("Layers")
        require(isinstance(saved_layers, list) and len(saved_layers) == len(layers) and
                all(isinstance(p, str) and re.fullmatch(r"[A-Za-z0-9_./-]+", p) and
                    not p.startswith("/") and ".." not in p.split("/") for p in saved_layers),
                "invalid classic layers")
        kind, manifest_id = "docker-classic", None
    else:
        # Containerd: .Id is a manifest digest, whose config digest is distinct.
        require(content_id(manifest_bytes) == image, "manifest image mismatch")
        require(isinstance(manifest, dict) and manifest.get("schemaVersion") == 2 and
                manifest.get("mediaType") in ("application/vnd.oci.image.manifest.v1+json",
                                             "application/vnd.docker.distribution.manifest.v2+json"),
                "unsupported image manifest (index not accepted)")
        descriptor = docker.get("Descriptor")
        require(isinstance(descriptor, dict) and descriptor.get("digest") == image and
                descriptor.get("mediaType") == manifest["mediaType"] and
                descriptor.get("size") == len(manifest_bytes), "Docker manifest descriptor mismatch")
        selected_config = manifest.get("config")
        require(isinstance(selected_config, dict) and selected_config.get("digest") == config_id and
                selected_config.get("size") == len(config_bytes) and selected_config.get("mediaType") in
                ("application/vnd.oci.image.config.v1+json", "application/vnd.docker.container.image.v1+json"),
                "manifest config mismatch")
        descriptors = manifest.get("layers")
        require(isinstance(descriptors, list) and len(descriptors) == len(layers) and
                all(isinstance(d, dict) and valid_id(d.get("digest")) and
                    type(d.get("size")) is int and d["size"] >= 0 and
                    isinstance(d.get("mediaType"), str) for d in descriptors), "invalid manifest layers")
        kind, manifest_id = "containerd-manifest", image
    scanner_identity = {}
    if raw is not None:
        require(isinstance(raw, dict), "invalid raw image report")
        metadata = raw.get("Metadata")
        require(isinstance(metadata, dict), "raw scan metadata missing")
        require(metadata.get("DiffIDs") == layers, "raw scan DiffIDs mismatch")
        scanned_layers = metadata.get("Layers")
        require(isinstance(scanned_layers, list) and
                all(isinstance(d, dict) and valid_id(d.get("Digest")) for d in scanned_layers) and
                [d.get("DiffID") for d in scanned_layers] == layers, "raw scan layer identity mismatch")
        if manifest_id is not None:
            require([d["Digest"] for d in scanned_layers] == [d["digest"] for d in descriptors],
                    "raw scan manifest layers mismatch")
        scanned_config = metadata.get("ImageConfig")
        require(isinstance(scanned_config, dict) and scanned_config.get("rootfs") == rootfs and
                scanned_config.get("os") == "linux" and scanned_config.get("architecture") == "amd64" and
                isinstance(scanned_config.get("config"), dict) and
                all(scanned_config["config"].get(k) == v for k, v in runtime.items()), "raw scan config mismatch")
        # Only now interpret the backend's ID surface. Both values originate
        # from the fully verified content-addressed chain, never tags/annotations
        # or an independently supplied fallback ID. Do not infer a version rule.
        scanned_id = metadata.get("ImageID")
        require(valid_id(scanned_id), "invalid raw scan image ID")
        if scanned_id == config_id:
            surface = "config"
        else:
            require(manifest_id is not None and scanned_id == manifest_id, "raw scan image mismatch")
            surface = "manifest"
        scanner_identity = {"trivyImageID": scanned_id, "trivyImageIDSurface": surface}
    return {"kind": kind, "dockerImageID": image, "manifestDigest": manifest_id,
            "configDigest": config_id, "diffIDs": layers, **scanner_identity}


def capture_identity(directory, image, archive):
    """Read docker-save stream without extracting/executing any layer or retaining its tar."""
    require(valid_id(image), "invalid local image ID")
    metadata, seen, total = {}, set(), 0
    with tarfile.open(fileobj=archive, mode="r|") as saved:
        for member in saved:
            require(member.name not in seen and len(seen) < 10000, "duplicate/oversized save inventory")
            seen.add(member.name)
            require(not member.issym() and not member.islnk(), "linked save member")
            if member.isdir():
                continue
            require(member.isfile(), "nonregular save member")
            candidate = (member.name == "manifest.json" or
                         re.fullmatch(r"(?:blobs/sha256/[a-f0-9]{64}|[a-f0-9]{64}\.json)", member.name))
            if candidate and member.size <= 1024 * 1024:
                total += member.size
                require(total <= 16 * 1024 * 1024, "save metadata budget exceeded")
                metadata[member.name] = saved.extractfile(member).read()
    manifest_path = "blobs/sha256/" + image[7:]
    if manifest_path in metadata:
        selected = identity_json(metadata[manifest_path])
        # An OCI config blob can also have the Docker classic ID as its name.
        if isinstance(selected, dict) and isinstance(selected.get("config"), dict) and valid_id(selected["config"].get("digest")):
            config_path = "blobs/sha256/" + selected["config"]["digest"][7:]
            manifest = metadata[manifest_path]
        else:
            config_path = manifest_path
            manifest = metadata.get("manifest.json")
    else:
        classic = identity_json(metadata.get("manifest.json", b"null"))
        require(isinstance(classic, list) and len(classic) == 1 and isinstance(classic[0], dict), "ambiguous saved image")
        config_path = classic[0].get("Config")
        manifest = metadata["manifest.json"]
    require(isinstance(config_path, str) and config_path in metadata and isinstance(manifest, bytes),
            "saved config/manifest missing")
    for name, data in (("image-config.json", metadata[config_path]), ("image-manifest.json", manifest)):
        with (directory / name).open("xb") as out:
            out.write(data)
    return image_identity(directory, image)


def stream(path):
    raw = path.read_text()
    decoder = json.JSONDecoder()
    offset = 0
    result = []
    while offset < len(raw):
        while offset < len(raw) and raw[offset].isspace():
            offset += 1
        if offset == len(raw):
            break
        value, offset = decoder.raw_decode(raw, offset)
        require(isinstance(value, dict), "non-object inventory entry")
        result.append(value)
    require(result, "empty inventory")
    return result


def binary_metadata(binary):
    # Reader is native, pinned and offline; the Linux target is never executed.
    image = "golang:1.26.9-bookworm@sha256:d9c68c2c51161e12fd77e4c6320687c9cd86e1af1e3ad6e6cd63ff970641453c"
    return subprocess.run([
        "docker", "run", "--rm", "--network", "none", "--read-only",
        "--mount", "type=bind,src=" + str(binary.resolve()) + ",target=/artifact/alloy,readonly",
        "--entrypoint", "/usr/local/go/bin/go", image, "version", "-m", "/artifact/alloy",
    ], check=True, capture_output=True, text=True, timeout=180).stdout


def normalized_metadata(info):
    lines = info.splitlines()
    require(lines and ": " in lines[0], "invalid build information")
    return [lines[0].split(": ", 1)[1], *lines[1:]]


def inspect(directory, binary, image, patches):
    require(not directory.is_symlink() and not binary.is_symlink(), "symlink evidence rejected")
    require(re.fullmatch(r"sha256:[a-f0-9]{64}", image), "invalid local image ID")
    digest = sha(binary)
    sums = (directory / "source-and-binary-sha256.txt").read_text().splitlines()
    require(sums[0].split() == [digest, "./build/alloy"], "binary hash mismatch")
    require((directory / "moby-base-sha.txt").read_text().strip() == BASE, "Moby source mismatch")
    for name, expected in PATCHES.items():
        require(sha(patches / name) == expected, "patch mismatch: " + name)
    info = (directory / "build-info.txt").read_text()
    actual = binary_metadata(binary)
    require(normalized_metadata(info) == normalized_metadata(actual), "embedded binary metadata mismatch")
    for setting in ("GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=1"):
        require(re.search(r"^\s*build\s+" + setting + r"$", info, re.M), "binary platform mismatch")
    require("go1.26.9" in info, "unexpected Go toolchain")
    require(re.search(r"dep\s+github.com/docker/docker\s+v28\.5\.2\+incompatible\s*\n\s*=>\s+/src/docker\s+\(devel\)", info), "binary Moby provenance mismatch")
    env = json.loads((directory / "go-env.json").read_text())
    require(all(env.get(k) == v for k, v in {
        "GOOS": "linux", "GOARCH": "amd64", "CGO_ENABLED": "1",
        "GOEXPERIMENT": "", "GOVERSION": "go1.26.9",
    }.items()), "inventory platform mismatch")
    packages = stream(directory / "packages.json")
    names = {p.get("ImportPath") for p in packages}
    require(len(names) == len(packages) and None not in names, "duplicate/missing packages")
    require(not any(p.get("Error") or p.get("DepsErrors") or p.get("Incomplete") for p in packages), "incomplete graph")
    require("github.com/grafana/alloy/otel_engine" in names, "collector entrypoint missing")
    require([p["ImportPath"] for p in packages if not p.get("DepOnly")] == ["github.com/grafana/alloy/otel_engine"], "unexpected graph entrypoint")
    by_name = {p["ImportPath"]: p for p in packages}
    root = "github.com/grafana/alloy/otel_engine"
    require(set(by_name[root].get("Deps", [])) == names - {root}, "root Deps coverage mismatch")
    # All imports must be represented: an arbitrary truncated graph cannot pass.
    for p in packages:
        imports = {p.get("ImportMap", {}).get(n, n) for n in p.get("Imports", [])}
        require(imports - {"C"} <= names, "missing imported package: " + p["ImportPath"])
        require(set(p.get("Deps", [])) <= names, "missing transitive dependency")
    # runtime/cgo is compiler-injected for CGO builds, rather than a Go import.
    pending = [root] + (["runtime/cgo"] if "runtime/cgo" in names else [])
    reached = set()
    while pending:
        name = pending.pop()
        if name in reached or name == "C":
            continue
        reached.add(name)
        p = by_name[name]
        pending.extend(p.get("ImportMap", {}).get(n, n) for n in p.get("Imports", []))
    require(reached == names, "unreachable inventory packages")
    moby = [p for p in packages if p.get("Module", {}).get("Path") == MOBY]
    require(moby, "Moby SDK inventory missing")
    require(all(p["Module"].get("Version") == VERSION and p["Module"].get("Replace", {}).get("Dir") == "/src/docker" for p in moby), "false Moby module version")
    modules = stream(directory / "modules.json")
    require(any(m.get("Path") == MOBY and m.get("Version") == VERSION and m.get("Replace", {}).get("Dir") == "/src/docker" for m in modules), "module graph mismatch")
    tests = stream(directory / "moby-authz-tests.json")
    require(not any(t.get("Action") == "fail" for t in tests), "AuthZ regression failed")
    require(any(t.get("Action") == "pass" and t.get("Package") == MOBY + "/pkg/authorization" and not t.get("Test") for t in tests), "AuthZ regression evidence missing")
    return digest, names


def supplement(image, digest):
    purl = "pkg:golang/github.com/docker/docker@v28.5.2%2Bincompatible"
    return {
        "bomFormat": "CycloneDX", "specVersion": "1.5", "version": 1,
        "serialNumber": "urn:uuid:" + str(uuid.uuid4()),
        "metadata": {"component": {
            "type": "application", "name": "SentinelOps Alloy",
            "properties": [
                {"name": "sentinelops:local-image-id", "value": image},
                {"name": "sentinelops:binary-sha256", "value": digest},
            ],
        }},
        "components": [{"type": "library", "name": MOBY, "version": VERSION,
                        "purl": purl, "bom-ref": purl,
                        "properties": [{"name": "sentinelops:source-sha", "value": BASE},
                                       {"name": "sentinelops:backport-sha", "value": BACKPORT}]}],
    }


def findings(report):
    require(isinstance(report.get("Results"), list), "scanner results missing")
    return [v for r in report["Results"] for v in r.get("Vulnerabilities", [])]


def assess(directory, names, image, digest):
    raw = json.loads((directory / "trivy-full.json").read_text())
    extra = json.loads((directory / "trivy-moby.json").read_text())
    identity = image_identity(directory, image, raw)
    full = findings(raw)
    require(any(r.get("Type") == "ubuntu" and r.get("Class") == "os-pkgs" and r.get("Packages") for r in raw["Results"]), "OS scan coverage missing")
    require(any(r.get("Type") == "gobinary" and r.get("Target") in ("usr/bin/alloy", "bin/alloy") and r.get("Packages") for r in raw["Results"]), "binary scan coverage missing")
    require(not any(v.get("Severity") in ("HIGH", "CRITICAL") for v in full), "full scan has HIGH/CRITICAL findings")
    extra_findings = findings(extra)
    require(extra_findings, "supplement scan unexpectedly empty; review database coverage")
    require(len(extra_findings) == len({v.get("VulnerabilityID") for v in extra_findings}), "duplicate supplementary advisory")
    evaluations = []
    for v in extra_findings:
        cve = v["VulnerabilityID"]
        require(v.get("PkgName") == MOBY and v.get("InstalledVersion") == VERSION, "supplement component mismatch")
        require(cve in ABSENT, "unreviewed SDK vulnerability: " + cve)
        prefix, function = ABSENT[cve]
        require(not any(n == MOBY + "/" + prefix or n.startswith(MOBY + "/" + prefix + "/") for n in names), "vulnerable package present: " + cve)
        evaluations.append({"id": cve, "severity": v["Severity"],
                            "assessment": "vulnerable_code_not_present", "function": function})
    require({v.get("VulnerabilityID") for v in extra_findings} == set(ABSENT), "supplement advisory coverage mismatch")
    return {"localImageID": image, "imageIdentity": identity, "binarySHA256": digest,
            "packagesSHA256": sha(directory / "packages.json"),
            "packageCount": len(names), "moby": evaluations,
            "residualRawFindings": full,
            "scope": "Only this compiled Alloy artifact; no external daemon or runtime acceptance. Not VEX and never a Trivy ignore file."}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("capture-identity", "prepare", "assess", "diagnose"))
    parser.add_argument("directory", type=Path)
    parser.add_argument("binary", type=Path)
    parser.add_argument("image")
    args = parser.parse_args()
    require(not args.directory.is_symlink(), "symlink directory rejected")
    args.directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    if args.mode == "capture-identity":
        print(json.dumps(capture_identity(args.directory, args.image, sys.stdin.buffer)))
        return
    patches = Path(__file__).resolve().parents[2] / "deploy/alloy/patches"
    if args.mode == "prepare":
        digest, _ = inspect(args.directory, args.binary, args.image, patches)
        output = supplement(args.image, digest)
        name = "moby-supplement.cdx.json"
    else:
        error = None
        try:
            require(args.mode != "diagnose", "security collection unavailable; inspect preceding build/scan failures")
            digest, names = inspect(args.directory, args.binary, args.image, patches)
            output = assess(args.directory, names, args.image, digest)
        except (ValueError, OSError, KeyError, TypeError, subprocess.SubprocessError) as exc:
            error = str(exc)
            output = {"error": error, "localImageID": args.image}
        # Preserve the complete raw evidence without exceeding the bounded local
        # artifact service. Each member can be independently decoded and hashed.
        evidence = {}
        for path in sorted(args.directory.iterdir()):
            if path.is_symlink() or path.name not in EVIDENCE_NAMES | {"alloy", "image-alloy.json"}:
                error = "unexpected or symlink evidence member: " + path.name
                continue
            if path.name in EVIDENCE_NAMES and path.is_file():
                data = path.read_bytes()
                evidence[path.name] = {"sha256": hashlib.sha256(data).hexdigest(),
                                       "encoding": "gzip+base64",
                                       "data": base64.b64encode(gzip.compress(data, mtime=0)).decode()}
        if error:
            output["error"] = error
        output = {"assessment": output, "rawEvidence": evidence}
        name = "image-alloy.json"
    require(not (args.directory / name).is_symlink(), "symlink output rejected")
    (args.directory / name).write_text(json.dumps(output, separators=(",", ":")) + "\n")
    if args.mode != "prepare" and error:
        sys.exit(error)


if __name__ == "__main__":
    main()

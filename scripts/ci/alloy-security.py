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
}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


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
    require(raw.get("Metadata", {}).get("ImageID") == image, "raw scan image mismatch")
    full = findings(raw)
    require(any(r.get("Type") == "ubuntu" and r.get("Class") == "os-pkgs" and r.get("Packages") for r in raw["Results"]), "OS scan coverage missing")
    require(any(r.get("Type") == "gobinary" and r.get("Target") in ("usr/bin/alloy", "bin/alloy") and r.get("Packages") for r in raw["Results"]), "binary scan coverage missing")
    require(not any(v.get("Severity") in ("HIGH", "CRITICAL") for v in full), "full scan has HIGH/CRITICAL findings")
    extra_findings = findings(extra)
    require(extra_findings, "supplement scan unexpectedly empty; review database coverage")
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
    return {"localImageID": image, "binarySHA256": digest,
            "packagesSHA256": sha(directory / "packages.json"),
            "packageCount": len(names), "moby": evaluations,
            "residualRawFindings": full,
            "scope": "Only this compiled Alloy artifact; no external daemon or runtime acceptance. Not VEX and never a Trivy ignore file."}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("prepare", "assess", "diagnose"))
    parser.add_argument("directory", type=Path)
    parser.add_argument("binary", type=Path)
    parser.add_argument("image")
    args = parser.parse_args()
    require(not args.directory.is_symlink(), "symlink directory rejected")
    args.directory.mkdir(parents=True, exist_ok=True, mode=0o700)
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

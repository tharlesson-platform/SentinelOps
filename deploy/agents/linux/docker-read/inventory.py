#!/usr/bin/env python3
"""Publica identidades Docker permitidas; preserva a última versão em erro."""

import argparse
import json
import os
from pathlib import Path
import socket
import subprocess

D = ["docker", "--host", "unix:///var/run/docker.sock"]


def run(args):
    env = {k: v for k, v in os.environ.items() if not k.startswith("DOCKER_")}
    return subprocess.check_output(D + args, text=True, timeout=30, env=env)


def atomic(path, data):
    encoded = (json.dumps(data, sort_keys=True, separators=(",", ":")) + "\n").encode()
    if path.exists() and path.read_bytes() == encoded:
        return
    temporary = path.with_suffix(".tmp")
    with os.fdopen(
        os.open(
            str(temporary), os.O_WRONLY | os.O_CREAT | os.O_TRUNC | os.O_NOFOLLOW, 0o600
        ),
        "wb",
    ) as f:
        f.write(encoded)
        f.flush()
        os.fsync(f.fileno())
    os.chown(temporary, 0, 473)
    os.chmod(temporary, 0o640)
    os.replace(temporary, path)
    directory = os.open(str(path.parent), os.O_DIRECTORY)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--expected-host", required=True)
    p.add_argument("--output-dir", required=True)
    a = p.parse_args()
    if socket.gethostname().split(".")[0] != a.expected_host:
        p.error("host mismatch")
    if (
        json.loads(run(["info", "--format", "{{json .}}"]))["Name"].split(".")[0]
        != a.expected_host
    ):
        p.error("docker daemon mismatch")
    ids = run(["ps", "-aq", "--no-trunc"]).split()
    cs = json.loads(run(["inspect", *ids])) if ids else []
    metadata = {}
    targets = []
    for c in cs:
        cid = c["Id"]
        name = c["Name"].lstrip("/")
        config = c.get("Config") or {}
        labels = config.get("Labels") or {}
        env = dict(x.split("=", 1) for x in (config.get("Env") or []) if "=" in x)
        service = (
            env.get("OTEL_SERVICE_NAME")
            or labels.get("com.docker.compose.service")
            or labels.get("com.docker.swarm.service.name")
            or name
        )
        metadata[cid] = {"container_name": name, "service_name": service}
        if c.get("HostConfig", {}).get("LogConfig", {}).get("Type") == "local":
            targets.append(
                {
                    "__meta_docker_container_id": cid,
                    "container_id": cid,
                    "container_name": name,
                    "service_name": service,
                    "host_name": a.expected_host,
                    "job": "docker-container",
                }
            )
    path = Path(a.output_dir)
    path.mkdir(mode=0o750, parents=True, exist_ok=True)
    if path.is_symlink():
        p.error("inventory directory cannot be a symlink")
    os.chown(path, 0, 473)
    # Keep stopped containers until Docker removes them: drain their final log lines.
    atomic(path / "metadata.json", metadata)
    atomic(
        path / "local-targets.json", sorted(targets, key=lambda t: t["container_id"])
    )
    print(
        json.dumps(
            {
                "host": a.expected_host,
                "containers": len(metadata),
                "localTargets": len(targets),
            }
        )
    )


if __name__ == "__main__":
    main()

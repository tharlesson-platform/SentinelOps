#!/usr/bin/env python3
"""Inventário somente leitura; não exporta conteúdo de logs, ambientes ou comandos."""

import argparse
import collections
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import re
import socket
import subprocess
import urllib.parse

DOCKER_SOCKET = "/var/run/docker.sock"


def run(argv):
    environment = os.environ.copy()
    if argv[0] == "docker":
        # Never follow an inherited context to another host's daemon.
        argv = ["docker", "--host", "unix://" + DOCKER_SOCKET, *argv[1:]]
        for key in (
            "DOCKER_HOST",
            "DOCKER_CONTEXT",
            "DOCKER_TLS_VERIFY",
            "DOCKER_CERT_PATH",
            "DOCKER_TLS",
        ):
            environment.pop(key, None)
    result = subprocess.run(
        argv, capture_output=True, text=True, timeout=30, env=environment
    )
    if result.returncode:
        raise RuntimeError(f"{argv[0]}: exit {result.returncode}")
    return result.stdout


def log_metadata(path, limit=262144, driver="json-file"):
    if not path:
        return {"state": "no_log_path"}
    try:
        file = Path(path)
        stat = file.stat()
        result = {
            "path": path,
            "bytes": stat.st_size,
            "modifiedAt": dt.datetime.fromtimestamp(
                stat.st_mtime, dt.timezone.utc
            ).isoformat(),
            "mode": oct(stat.st_mode & 0o777),
            "uid": stat.st_uid,
            "gid": stat.st_gid,
        }
        if driver != "json-file":
            return dict(
                result,
                state="metadata_only",
                reason="log_driver_requires_specific_reader",
            )
        # A bounded suffix proves emission only; it is not the whole history.
        with file.open("rb") as stream:
            offset = max(0, stat.st_size - limit)
            stream.seek(offset)
            raw = stream.read(limit)
        lines = raw.splitlines()[1:] if offset else raw.splitlines()
        timestamps, streams, errors, responses = (
            [],
            collections.Counter(),
            0,
            collections.Counter(),
        )
        parsed = 0
        for line in lines:
            try:
                record = json.loads(line)
            except (ValueError, UnicodeError):
                continue
            if not isinstance(record, dict):
                continue
            parsed += 1
            stamp = record.get("time")
            if isinstance(stamp, str):
                try:
                    # Python 3.8/3.10 reject Docker's nanosecond fractions.
                    normalized = re.sub(r"(\.\d{6})\d+(?=Z|[+-]|$)", r"\1", stamp)
                    value = dt.datetime.fromisoformat(normalized.replace("Z", "+00:00"))
                    if value.tzinfo is not None:
                        timestamps.append(value.astimezone(dt.timezone.utc).isoformat())
                except ValueError:
                    pass
            if record.get("stream") in ("stdout", "stderr"):
                streams[record["stream"]] += 1
            message = record.get("log", "")
            if isinstance(message, str):
                errors += bool(
                    re.search(r"\b(error|exception|fatal|panic)\b", message, re.I)
                )
                # Do not guess status from arbitrary digits in free-form messages.
                try:
                    structured = json.loads(message)
                except (ValueError, TypeError):
                    structured = {}
                if isinstance(structured, dict):
                    status = next(
                        (
                            structured[k]
                            for k in (
                                "http.response.status_code",
                                "http.status_code",
                                "statusCode",
                                "status",
                            )
                            if k in structured
                        ),
                        None,
                    )
                    if isinstance(status, (int, str)) and re.fullmatch(
                        r"[45]\d\d", str(status)
                    ):
                        responses[str(status)[0] + "xx"] += 1
        result.update(
            state="readable",
            sampledBytes=len(raw),
            suffixOnly=bool(offset),
            parsedJSONRecords=parsed,
            firstTimestamp=min(timestamps) if timestamps else None,
            lastTimestamp=max(timestamps) if timestamps else None,
            streams=dict(streams),
            errorTextMatches=errors,
            structuredHTTPStatusCounts=dict(responses),
        )
        return result
    except OSError as error:
        return {"path": path, "state": "read_error", "errno": error.errno}


def endpoint_metadata(value):
    try:
        parsed = urllib.parse.urlsplit(value)
        return {
            "scheme": parsed.scheme,
            "host": parsed.hostname,
            "port": parsed.port,
            "path": (
                parsed.path
                if parsed.path
                in (
                    "",
                    "/",
                    "/v1/traces",
                    "/v1/metrics",
                    "/v1/logs",
                    "/api/v1/push",
                    "/api/v1/write",
                )
                else "<custom>"
            ),
            "hasCredentials": bool(parsed.username or parsed.password),
            "hasQuery": bool(parsed.query),
        }
    except ValueError:
        return {"invalid": True}


def inspect_container(c, default_driver):
    config, host = c.get("Config", {}), c.get("HostConfig", {})
    labels = config.get("Labels") or {}
    env = dict(item.split("=", 1) for item in (config.get("Env") or []) if "=" in item)
    state = c.get("State", {})
    name = c["Name"].lstrip("/")
    agent = bool(
        re.search(
            r"sentinelops.*(alloy|logs|metrics|beyla|collector)|otel.*collector", name
        )
    )
    item = {
        "id": c["Id"],
        "name": name,
        "image": config.get("Image"),
        "imageId": c.get("Image"),
        "status": state.get("Status"),
        "health": state.get("Health", {}).get("Status"),
        "restarts": c.get("RestartCount"),
        "startedAt": state.get("StartedAt"),
        "pid": state.get("Pid"),
        "composeProject": labels.get("com.docker.compose.project"),
        "composeService": labels.get("com.docker.compose.service"),
        "composeWorkingDir": labels.get("com.docker.compose.project.working_dir"),
        "composeConfigFiles": labels.get("com.docker.compose.project.config_files"),
        "logDriver": host.get("LogConfig", {}).get("Type") or default_driver,
        "logRotation": {
            k: v
            for k, v in host.get("LogConfig", {}).get("Config", {}).items()
            if k in ("max-size", "max-file", "compress")
        },
        "exposedPorts": sorted((config.get("ExposedPorts") or {}).keys()),
        "publishedPorts": c.get("NetworkSettings", {}).get("Ports"),
        "log": log_metadata(
            c.get("LogPath", ""),
            driver=host.get("LogConfig", {}).get("Type") or default_driver,
        ),
        "mounts": [
            {
                "source": m.get("Source"),
                "destination": m.get("Destination"),
                "readWrite": m.get("RW"),
                "type": m.get("Type"),
            }
            for m in c.get("Mounts", [])
        ],
        "otel": {
            "serviceName": env.get("OTEL_SERVICE_NAME"),
            "tracesSampler": env.get("OTEL_TRACES_SAMPLER"),
            "samplerArgument": env.get("OTEL_TRACES_SAMPLER_ARG"),
            "nodeAgentConfigured": "opentelemetry"
            in env.get("NODE_OPTIONS", "").lower(),
            "javaAgentConfigured": "opentelemetry"
            in env.get("JAVA_TOOL_OPTIONS", "").lower(),
            "dotnetProfilerEnabled": env.get("CORECLR_ENABLE_PROFILING") == "1",
            "exportEndpoints": {
                k: endpoint_metadata(v)
                for k, v in env.items()
                if k.startswith("OTEL_EXPORTER_OTLP") and k.endswith("ENDPOINT")
            },
        },
        "collector": agent,
    }
    if state.get("Running"):
        try:
            # Docker needs the PID column to associate ps rows with a container.
            rows = run(["docker", "top", c["Id"], "-eo", "pid,comm"]).splitlines()[1:]
            item["processNames"] = sorted(
                {
                    row.split(None, 1)[1].strip()
                    for row in rows
                    if len(row.split(None, 1)) == 2
                }
            )
        except (RuntimeError, subprocess.TimeoutExpired):
            item["processNamesError"] = "unavailable"
    if agent:
        item["collectorAccess"] = {
            "user": config.get("User"),
            "privileged": host.get("Privileged"),
            "capAdd": host.get("CapAdd"),
            "pidMode": host.get("PidMode"),
            "networkMode": host.get("NetworkMode"),
            "storagePathConfigured": any(
                "storage.path" in x for x in (config.get("Cmd") or [])
            ),
        }
        item["collectorConfigHashes"] = {}
        for mount in c.get("Mounts", []):
            source = Path(mount.get("Source") or "/nonexistent")
            if source.suffix in (".alloy", ".yml", ".yaml") and source.is_file():
                try:
                    item["collectorConfigHashes"][str(source)] = hashlib.sha256(
                        source.read_bytes()
                    ).hexdigest()
                except OSError:
                    item["collectorConfigHashes"][str(source)] = "unreadable"
        item["collectorDestinations"] = {
            k: endpoint_metadata(env[k])
            for k in (
                "SENTINEL_LOGS_ENDPOINT",
                "SENTINEL_METRICS_ENDPOINT",
                "SENTINEL_OTLP_ENDPOINT",
            )
            if k in env
        }
        item["collectorHost"] = env.get("SENTINEL_HOST_NAME")
    return item


def main():
    global DOCKER_SOCKET
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--expected-host", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument(
        "--docker-socket",
        default="/var/run/docker.sock",
        help="Socket Unix local; contextos e daemons remotos não são aceitos",
    )
    args = parser.parse_args()
    actual = socket.gethostname().split(".")[0]
    if actual != args.expected_host:
        parser.error(
            f"Host divergente: esperado {args.expected_host}, observado {actual}; nenhuma coleta executada"
        )
    socket_path = Path(args.docker_socket)
    if not socket_path.is_absolute() or not socket_path.is_socket():
        parser.error(
            "--docker-socket precisa apontar para um socket Unix local existente"
        )
    DOCKER_SOCKET = str(socket_path)
    os.umask(0o077)
    info = json.loads(run(["docker", "info", "--format", "{{json .}}"]))
    daemon_name = info.get("Name", "")
    if daemon_name.split(".")[0] != actual:
        parser.error(
            f"Daemon Docker divergente: {daemon_name}; inventário de containers não executado"
        )
    ids = run(["docker", "ps", "-aq", "--no-trunc"]).split()
    containers = json.loads(run(["docker", "inspect", *ids])) if ids else []
    root = info.get("DockerRootDir")
    disk = os.statvfs(root)
    output = {
        "collectedAt": dt.datetime.now(dt.timezone.utc).isoformat(),
        "hostname": actual,
        "kernel": os.uname().release,
        "euid": os.geteuid(),
        "dockerVersion": info.get("ServerVersion"),
        "dockerEndpoint": "unix://" + DOCKER_SOCKET,
        "dockerDaemonName": daemon_name,
        "dockerRootDir": root,
        "defaultLogDriver": info.get("LoggingDriver"),
        "dockerFilesystem": {
            "capacityBytes": disk.f_blocks * disk.f_frsize,
            "availableBytes": disk.f_bavail * disk.f_frsize,
        },
        "totalContainers": len(containers),
        "runningContainers": sum(
            c.get("State", {}).get("Running", False) for c in containers
        ),
        "containers": [
            inspect_container(c, info.get("LoggingDriver")) for c in containers
        ],
    }
    try:
        stats = [
            json.loads(line)
            for line in run(
                ["docker", "stats", "--no-stream", "--format", "{{json .}}"]
            ).splitlines()
        ]
        output["dockerStats"] = [
            {
                k: row.get(k)
                for k in (
                    "Name",
                    "CPUPerc",
                    "MemUsage",
                    "MemPerc",
                    "PIDs",
                    "NetIO",
                    "BlockIO",
                )
            }
            for row in stats
        ]
    except (RuntimeError, subprocess.TimeoutExpired, ValueError):
        output["dockerStatsError"] = "unavailable"
    destination = Path(args.output)
    destination.parent.mkdir(parents=True, exist_ok=True)
    with destination.open("x") as file:
        file.write(json.dumps(output, indent=2))
    destination.chmod(0o600)
    print(
        json.dumps(
            {
                "host": actual,
                "total": output["totalContainers"],
                "running": output["runningContainers"],
                "file": str(destination),
                "sha256": hashlib.sha256(destination.read_bytes()).hexdigest(),
            }
        )
    )


if __name__ == "__main__":
    main()

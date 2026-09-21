#!/usr/bin/env python3
"""API Docker local restrita: metadados permitidos e logs apenas do driver local."""

import argparse
import datetime as dt
import http.client
import http.server
import json
import os
from pathlib import Path
import re
import select
import socket
import socketserver
import threading
import urllib.parse

DOCKER_SOCKET = "/var/run/docker.sock"
LABELS = (
    "com.docker.compose.project",
    "com.docker.compose.service",
    "com.docker.swarm.service.name",
)


class DockerConnection(http.client.HTTPConnection):
    def __init__(self):
        super().__init__("localhost", timeout=60)

    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(self.timeout)
        self.sock.connect(DOCKER_SOCKET)


def docker_json(path):
    connection = DockerConnection()
    try:
        connection.request("GET", path)
        response = connection.getresponse()
        if response.status != 200:
            raise RuntimeError("docker_read_failed")
        data = response.read(16 * 1024 * 1024 + 1)
        if len(data) > 16 * 1024 * 1024:
            raise RuntimeError("docker_response_limit")
        return json.loads(data)
    finally:
        connection.close()


def service_labels(config):
    labels = config.get("Labels") or {}
    return {key: labels[key] for key in LABELS if key in labels}


def safe_inspect(container):
    config = container.get("Config") or {}
    return {
        "Id": container["Id"],
        "Name": container["Name"],
        "Config": {"Labels": service_labels(config), "Tty": bool(config.get("Tty"))},
        "State": {
            "Running": bool(container.get("State", {}).get("Running")),
            "FinishedAt": container.get("State", {}).get(
                "FinishedAt", "0001-01-01T00:00:00Z"
            ),
        },
        "NetworkSettings": {"Networks": {}, "Ports": {}},
    }


def route(path, mode):
    parsed = urllib.parse.urlsplit(path)
    if parsed.scheme or parsed.netloc or parsed.fragment or "%" in parsed.path:
        raise ValueError("invalid_path")
    clean = re.sub(r"^/v1\.\d{1,3}(?=/)", "", parsed.path)
    if clean in ("/_ping", "/version", "/info") and not parsed.query:
        return clean, None, {}
    match = re.fullmatch(r"/containers/([a-f0-9]{12,64})/(json|logs)", clean)
    if not match or (match[2] == "logs" and mode != "logs"):
        raise ValueError("route_denied")
    if match[2] == "json" and parsed.query:
        raise ValueError("query_denied")
    query = urllib.parse.parse_qs(parsed.query, keep_blank_values=True)
    if any(len(v) != 1 for v in query.values()):
        raise ValueError("duplicate_query")
    allowed = {
        "stdout",
        "stderr",
        "timestamps",
        "follow",
        "since",
        "until",
        "tail",
        "details",
    }
    if set(query) - allowed:
        raise ValueError("query_denied")
    return match[2], match[1], {k: v[0] for k, v in query.items()}


class Policy:
    def __init__(self, path):
        self.path = Path(path)
        self.lock = threading.Lock()
        self.activated_at = json.loads(self.path.with_name("activated-at.json").read_text())
        if type(self.activated_at) is not int or self.activated_at <= 0:
            raise ValueError("invalid_activation_state")
        self.enrolled = json.loads(self.path.read_text())
        if not isinstance(self.enrolled, dict) or any(
            not re.fullmatch(r"[a-f0-9]{64}", key)
            or type(value) is not int
            or value <= 0
            for key, value in self.enrolled.items()
        ):
            raise ValueError("invalid_enrollment_state")

    def since(self, container_id, created_at):
        with self.lock:
            if container_id not in self.enrolled:
                updated = dict(self.enrolled)
                if type(created_at) is not int or created_at <= 0:
                    raise ValueError("invalid_container_creation")
                # Existing history stays excluded; new containers retain startup logs
                # even when discovery happens after creation.
                updated[container_id] = max(self.activated_at, created_at)
                temporary = self.path.with_suffix(".tmp")
                with temporary.open("w") as file:
                    json.dump(updated, file)
                    file.flush()
                    os.fsync(file.fileno())
                os.replace(temporary, self.path)
                directory = os.open(str(self.path.parent), os.O_DIRECTORY)
                try:
                    os.fsync(directory)
                finally:
                    os.close(directory)
                self.enrolled = updated
            return self.enrolled[container_id]


def log_query(query, floor):
    # Docker client sends Unix seconds; reject ambiguity rather than widening history.
    requested = query.get("since", "0")
    if not re.fullmatch(r"\d+(?:\.\d{1,9})?", requested):
        raise ValueError("invalid_since")
    seconds = int(requested.split(".")[0])
    since = requested if seconds >= floor else str(floor)
    result = {
        "since": since,
        "stdout": "1",
        "stderr": "1",
        "timestamps": "1",
        "follow": "1",
        "tail": "all",
        "details": "0",
    }
    return urllib.parse.urlencode(result)


class Handler(http.server.BaseHTTPRequestHandler):
    server_version = "SentinelOpsReadOnly"

    def setup(self):
        self.request.settimeout(10)
        super().setup()

    def log_message(self, fmt, *args):
        # Never journal request data, container logs or upstream bodies.
        pass

    def reply(self, status, value, content_type="application/json"):
        data = value if isinstance(value, bytes) else json.dumps(value).encode()
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(data)))
        self.send_header("API-Version", self.server.version["ApiVersion"])
        self.end_headers()
        if self.command != "HEAD":
            self.wfile.write(data)

    def do_HEAD(self):
        if self.path != "/_ping":
            return self.reply(403, {"message": "route_denied"})
        self.reply(200, b"OK", "text/plain")

    def do_GET(self):
        try:
            kind, cid, query = route(self.path, self.server.mode)
            if kind == "/_ping":
                return self.reply(200, b"OK", "text/plain")
            if kind == "/version":
                return self.reply(200, self.server.version)
            if kind == "/info":
                info = docker_json("/info")
                return self.reply(
                    200,
                    {
                        k: info.get(k)
                        for k in (
                            "Name",
                            "Driver",
                            "ServerVersion",
                            "CgroupDriver",
                            "CgroupVersion",
                        )
                    },
                )
            c = docker_json("/containers/" + cid + "/json")
            if kind == "json":
                return self.reply(200, safe_inspect(c))
            if c.get("HostConfig", {}).get("LogConfig", {}).get("Type") != "local":
                return self.reply(403, {"message": "driver_denied"})
            created = dt.datetime.fromisoformat(
                re.sub(r"(\.\d{6})\d+", r"\1", c["Created"]).replace("Z", "+00:00")
            )
            if created.tzinfo is None:
                raise ValueError("invalid_container_creation")
            encoded = log_query(
                query, self.server.policy.since(c["Id"], int(created.timestamp()))
            )
            connection = DockerConnection()
            try:
                connection.request("GET", "/containers/" + c["Id"] + "/logs?" + encoded)
                upstream_socket = connection.sock
                response = connection.getresponse()
                if response.status != 200:
                    return self.reply(502, {"message": "log_read_failed"})
                upstream_socket.settimeout(None)
                done = threading.Event()

                def cancel_upstream():
                    while not done.wait(0.5):
                        readable, _, _ = select.select([self.connection], [], [], 0)
                        if readable:
                            try:
                                upstream_socket.shutdown(socket.SHUT_RDWR)
                            except OSError:
                                pass
                            return

                threading.Thread(target=cancel_upstream, daemon=True).start()
                self.send_response(200)
                self.send_header(
                    "Content-Type",
                    response.getheader(
                        "Content-Type", "application/vnd.docker.raw-stream"
                    ),
                )
                self.send_header("Connection", "close")
                self.end_headers()
                self.close_connection = True
                while True:
                    chunk = response.read1(65536)
                    if not chunk:
                        break
                    self.wfile.write(chunk)
                    self.wfile.flush()
            finally:
                if "done" in locals():
                    done.set()
                connection.close()
        except ValueError:
            self.reply(403, {"message": "request_denied"})
        except (BrokenPipeError, ConnectionResetError, socket.timeout):
            self.close_connection = True
        except Exception:
            # Fixed diagnostic text only; no upstream exception/body can leak.
            print("docker_read_failed", flush=True)
            self.close_connection = True

    def do_POST(self):
        self.reply(403, {"message": "method_denied"})

    do_PUT = do_POST
    do_PATCH = do_POST
    do_DELETE = do_POST


class Server(socketserver.ThreadingMixIn, socketserver.UnixStreamServer):
    daemon_threads = True
    request_queue_size = 32

    def __init__(self, *args):
        self.slots = threading.BoundedSemaphore(32)
        super().__init__(*args)

    def process_request(self, request, client_address):
        if not self.slots.acquire(blocking=False):
            request.close()
            return
        try:
            super().process_request(request, client_address)
        except Exception:
            self.slots.release()
            raise

    def process_request_thread(self, request, client_address):
        try:
            super().process_request_thread(request, client_address)
        finally:
            self.slots.release()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--expected-host", required=True)
    parser.add_argument("--mode", choices=("metadata", "logs"), required=True)
    parser.add_argument("--socket", required=True)
    parser.add_argument(
        "--state", default="/var/lib/sentinelops-docker-read/enrolled.json"
    )
    args = parser.parse_args()
    if socket.gethostname().split(".")[0] != args.expected_host:
        parser.error("host mismatch")
    if docker_json("/info").get("Name", "").split(".")[0] != args.expected_host:
        parser.error("docker daemon mismatch")
    policy = Policy(args.state) if args.mode == "logs" else None
    os.umask(0o077)
    path = Path(args.socket)
    if path.exists():
        if not path.is_socket():
            parser.error("socket path is not a socket")
        path.unlink()
    with Server(str(path), Handler) as server:
        os.chown(path, 0, 473)
        os.chmod(path, 0o660)
        server.mode = args.mode
        version = docker_json("/version")
        server.version = {
            k: version[k]
            for k in ("ApiVersion", "MinAPIVersion", "Version", "Os", "Arch")
            if k in version
        }
        server.policy = policy
        print("docker_read_ready mode=" + args.mode, flush=True)
        server.serve_forever()


if __name__ == "__main__":
    main()

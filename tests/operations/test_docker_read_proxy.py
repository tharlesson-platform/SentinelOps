import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location(
    "proxy",
    Path(__file__).resolve().parents[2] / "deploy/agents/linux/docker-read/proxy.py",
)
proxy = importlib.util.module_from_spec(spec)
spec.loader.exec_module(proxy)


class ProxyTest(unittest.TestCase):
    def test_minimal_inspect_keeps_client_contract_not_secrets(self):
        source = {
            "Id": "a" * 64,
            "Name": "/app",
            "Config": {
                "Env": ["TOKEN=private"],
                "Cmd": ["private"],
                "Tty": False,
                "Labels": {"secret": "private", "com.docker.compose.service": "app"},
            },
            "State": {
                "Running": True,
                "FinishedAt": "0001-01-01T00:00:00Z",
                "Health": {"Log": [{"Output": "private"}]},
            },
        }
        out = proxy.safe_inspect(source)
        self.assertNotIn("private", json.dumps(out))
        self.assertEqual(
            out["State"], {"Running": True, "FinishedAt": "0001-01-01T00:00:00Z"}
        )
        self.assertFalse(out["Config"]["Tty"])
        self.assertEqual(out["Config"]["Labels"], {"com.docker.compose.service": "app"})

    def test_route_boundary(self):
        for path in [
            "/containers/json",
            "/networks",
            "/events",
            "/images/json",
            "/containers/" + "a" * 64 + "/archive",
            "http://other/info",
            "/%69nfo",
            "/info?x=1",
            "/containers/" + "a" * 64 + "/logs?since=1&since=2",
            "/containers/" + "a" * 64 + "/logs?unknown=1",
            "/containers/../info",
            "/v1.53/containers/" + "a" * 64 + "/json?size=1",
        ]:
            with self.subTest(path=path), self.assertRaises(ValueError):
                proxy.route(path, "logs")
        self.assertEqual(proxy.route("/v1.53/info", "metadata"), ("/info", None, {}))
        self.assertEqual(
            proxy.route("/v1.53/containers/" + "a" * 64 + "/json", "metadata")[0],
            "json",
        )
        with self.assertRaises(ValueError):
            proxy.route("/containers/" + "a" * 64 + "/logs", "metadata")

    def test_logs_floor_does_not_cut_newer_checkpoint_or_tail_history(self):
        from urllib.parse import parse_qs

        out = parse_qs(
            proxy.log_query({"since": "0", "tail": "10", "details": "true"}, 100)
        )
        self.assertEqual(out["since"], ["100"])
        self.assertEqual(out["tail"], ["all"])
        self.assertEqual(out["details"], ["0"])
        self.assertEqual(
            parse_qs(proxy.log_query({"since": "101.123456789"}, 100))["since"],
            ["101.123456789"],
        )
        for value in ["-1", "yesterday", "1e3", "NaN", "1&tail=all"]:
            with self.assertRaises(ValueError):
                proxy.log_query({"since": value}, 100)

    def test_failed_persistence_never_authorizes_from_memory(self):
        for failure in ["replace", "fsync"]:
            with tempfile.TemporaryDirectory() as folder:
                path = Path(folder) / "state.json"
                path.write_text("{}")
                path.with_name("activated-at.json").write_text("100")
                policy = proxy.Policy(path)
                with patch.object(proxy.os, failure, side_effect=OSError("full")):
                    for _ in range(2):
                        with self.assertRaises(OSError):
                            policy.since("a" * 64, 50)
                        self.assertNotIn("a" * 64, policy.enrolled)
                stamp = policy.since("a" * 64, 50)
                self.assertEqual(proxy.Policy(path).since("a" * 64, 50), stamp)

    def test_missing_or_corrupt_state_stops_instead_of_resetting_enrollment(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "state.json"
            with self.assertRaises(FileNotFoundError):
                proxy.Policy(path)
            path.with_name("activated-at.json").write_text("100")
            path.write_text("broken")
            with self.assertRaises(ValueError):
                proxy.Policy(path)
            path.write_text('{"not-an-id": 100}')
            with self.assertRaises(ValueError):
                proxy.Policy(path)

    def test_enrollment_is_durable_and_stable_after_restart(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "state.json"
            path.write_text("{}")
            path.with_name("activated-at.json").write_text("100")
            policy = proxy.Policy(path)
            first = policy.since("a" * 64, 50)
            self.assertEqual(proxy.Policy(path).since("a" * 64, 50), first)
            self.assertEqual(json.loads(path.read_text()), {"a" * 64: first})


    def test_new_container_keeps_startup_without_backfilling_pre_activation(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "state.json"
            path.write_text("{}")
            path.with_name("activated-at.json").write_text("100")
            policy = proxy.Policy(path)
            self.assertEqual(policy.since("a" * 64, 50), 100)
            self.assertEqual(policy.since("b" * 64, 120), 120)
            # A recreated container has a new identity and its own creation floor.
            self.assertEqual(proxy.Policy(path).since("c" * 64, 140), 140)
            self.assertEqual(proxy.Policy(path).since("a" * 64, 50), 100)
            path.with_name("activated-at.json").unlink()
            with self.assertRaises(FileNotFoundError):
                proxy.Policy(path)


if __name__ == "__main__":
    unittest.main()


class ProxyHTTPTest(unittest.TestCase):
    def test_real_unix_http_denies_mutations_and_keeps_stream_frames(self):
        import http.client
        import http.server
        import socket
        import socketserver
        import struct
        import threading

        class Connection(http.client.HTTPConnection):
            def __init__(self, path):
                super().__init__("local", timeout=2)
                self.path = path

            def connect(self):
                self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
                self.sock.settimeout(2)
                self.sock.connect(self.path)

        frames = b"".join(
            struct.pack(">BxxxI", s, len(msg)) + msg
            for s, msg in [
                (1, b"2026-09-21T00:00:00Z test stdout\n"),
                (2, b"2026-09-21T00:00:00Z test stderr\n"),
            ]
        )
        observed = []

        class Upstream(http.server.BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_GET(self):
                observed.append(self.path)
                if "/logs?" in self.path:
                    self.send_response(200)
                    self.send_header(
                        "Content-Type", "application/vnd.docker.raw-stream"
                    )
                    self.send_header("Content-Length", str(len(frames)))
                    self.end_headers()
                    self.wfile.write(frames)
                    return
                data = json.dumps(
                    {
                        "Id": "a" * 64,
                        "Name": "/app",
                        "Created": "2026-09-20T00:00:00.123456789Z",
                        "Config": {"Tty": False, "Env": ["SECRET=private"]},
                        "State": {
                            "Running": True,
                            "FinishedAt": "0001-01-01T00:00:00Z",
                        },
                        "HostConfig": {"LogConfig": {"Type": "local"}},
                    }
                ).encode()
                self.send_response(200)
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)

        with tempfile.TemporaryDirectory(dir="/tmp") as folder:
            folder = Path(folder)
            state = folder / "state.json"
            state.write_text("{}")
            state.with_name("activated-at.json").write_text("100")
            upstream = socketserver.UnixStreamServer(
                str(folder / "docker.sock"), Upstream
            )
            server = proxy.Server(str(folder / "proxy.sock"), proxy.Handler)
            server.version = {"ApiVersion": "1.53"}
            server.mode = "logs"
            server.policy = proxy.Policy(state)
            for instance in [upstream, server]:
                threading.Thread(target=instance.serve_forever, daemon=True).start()
            try:
                with patch.object(proxy, "DOCKER_SOCKET", str(folder / "docker.sock")):

                    def request(method, path):
                        c = Connection(str(folder / "proxy.sock"))
                        c.request(method, path)
                        r = c.getresponse()
                        out = (r.status, r.read())
                        c.close()
                        return out

                    self.assertEqual(request("HEAD", "/_ping"), (200, b""))
                    self.assertEqual(request("POST", "/containers/create")[0], 403)
                    self.assertEqual(request("GET", "/containers/json")[0], 403)
                    self.assertEqual(request("GET", "http://evil/info")[0], 403)
                    status, body = request("GET", "/containers/" + "a" * 64 + "/json")
                    self.assertEqual(status, 200)
                    self.assertNotIn(b"private", body)
                    status, body = request(
                        "GET", "/containers/" + "a" * 64 + "/logs?since=0&details=true"
                    )
                    self.assertEqual(status, 200)
                    self.assertEqual(body, frames)
                    self.assertIn("details=0", observed[-1])
                    self.assertNotIn("since=0", observed[-1])
            finally:
                for instance in [server, upstream]:
                    instance.shutdown()
                    instance.server_close()

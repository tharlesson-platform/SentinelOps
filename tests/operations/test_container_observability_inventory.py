import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

MODULE = (
    Path(__file__).resolve().parents[2] / "scripts/inspect-container-observability.py"
)
spec = importlib.util.spec_from_file_location("container_inventory", MODULE)
inventory = importlib.util.module_from_spec(spec)
spec.loader.exec_module(inventory)


class ContainerInventoryTest(unittest.TestCase):
    def test_logs_export_counters_not_contents_and_normalize_time(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "container-json.log"
            path.write_text(
                "not-json\n"
                + json.dumps(
                    {
                        "time": "2026-09-21T09:00:00-03:00",
                        "stream": "stderr",
                        "log": json.dumps(
                            {
                                "statusCode": 503,
                                "message": "ERROR password=never-export-this",
                            }
                        ),
                    }
                )
                + "\n"
            )
            result = inventory.log_metadata(str(path))
            self.assertEqual(result["lastTimestamp"], "2026-09-21T12:00:00+00:00")
            self.assertEqual(result["structuredHTTPStatusCounts"], {"5xx": 1})
            self.assertEqual(result["errorTextMatches"], 1)
            self.assertEqual(result["parsedJSONRecords"], 1)
            self.assertNotIn("never-export-this", json.dumps(result))
            self.assertNotIn("password", json.dumps(result))

    def test_suffix_limits_and_unstructured_digits_not_http_status(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "test.log"
            path.write_text(
                "x" * 400
                + "\n"
                + json.dumps(
                    {"stream": "stdout", "log": "invoice 404 not an HTTP response"}
                )
                + "\n"
            )
            result = inventory.log_metadata(str(path), limit=200)
            self.assertTrue(result["suffixOnly"])
            self.assertEqual(result["sampledBytes"], 200)
            self.assertEqual(result["structuredHTTPStatusCounts"], {})
            self.assertEqual(result["parsedJSONRecords"], 1)

    def test_unknown_driver_not_treated_as_json_log(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "local.log"
            path.write_bytes(b"binary")
            result = inventory.log_metadata(str(path), driver="local")
            self.assertEqual(result["state"], "metadata_only")
            self.assertNotIn("parsedJSONRecords", result)
        self.assertEqual(inventory.log_metadata("")["state"], "no_log_path")
        self.assertEqual(
            inventory.log_metadata("/nonexistent/inventory-test")["state"], "read_error"
        )

    def test_endpoint_does_not_export_credentials_query_or_custom_path(self):
        result = inventory.endpoint_metadata(
            "https://secret-user:secret-password@example.test/private-token?key=secret-key"
        )
        self.assertEqual(result["host"], "example.test")
        self.assertEqual(result["path"], "<custom>")
        self.assertTrue(result["hasCredentials"])
        self.assertTrue(result["hasQuery"])
        self.assertNotIn("secret", json.dumps(result))
        self.assertNotIn("private-token", json.dumps(result))

    def test_container_does_not_export_environment_labels_command_or_health_output(
        self,
    ):
        fixture = {
            "Id": "c" * 64,
            "Name": "/app",
            "Config": {
                "Image": "test",
                "Env": [
                    "PASSWORD=private-password",
                    "OTEL_SERVICE_NAME=app",
                    "JAVA_TOOL_OPTIONS=-javaagent:opentelemetry.jar -Dsecret=private-password",
                ],
                "Labels": {
                    "com.docker.compose.service": "app",
                    "secret-label": "private-label",
                },
                "Cmd": ["--password", "private-command"],
            },
            "HostConfig": {
                "LogConfig": {
                    "Type": "json-file",
                    "Config": {"max-size": "10m", "secret": "private-option"},
                }
            },
            "State": {
                "Status": "exited",
                "Health": {"Status": "healthy", "Log": [{"Output": "private-health"}]},
            },
        }
        result = inventory.inspect_container(fixture, "json-file")
        self.assertNotIn("private-", json.dumps(result))
        self.assertTrue(result["otel"]["javaAgentConfigured"])
        self.assertEqual(result["composeService"], "app")

    def test_wrong_host_stops_before_docker_or_output(self):
        with patch(
            "sys.argv",
            [
                "inventory",
                "--expected-host",
                "approved-host",
                "--output",
                "/tmp/not-written",
            ],
        ), patch.object(
            inventory.socket, "gethostname", return_value="other-host"
        ), patch.object(
            inventory, "run"
        ) as runner:
            with self.assertRaises(SystemExit):
                inventory.main()
            runner.assert_not_called()

    def test_remote_docker_context_is_never_used(self):
        with patch.dict(
            inventory.os.environ,
            {
                "DOCKER_HOST": "tcp://remote:2375",
                "DOCKER_CONTEXT": "production-other-host",
                "DOCKER_TLS_VERIFY": "1",
            },
        ), patch.object(inventory.subprocess, "run") as process:
            process.return_value.returncode = 0
            process.return_value.stdout = "{}"
            inventory.run(["docker", "info"])
            args, kwargs = process.call_args
            self.assertEqual(
                args[0][:3], ["docker", "--host", "unix:///var/run/docker.sock"]
            )
            self.assertNotIn("DOCKER_CONTEXT", kwargs["env"])
            self.assertNotIn("DOCKER_HOST", kwargs["env"])
            self.assertNotIn("DOCKER_TLS_VERIFY", kwargs["env"])

    def test_wrong_daemon_stops_before_listing_containers(self):
        with patch(
            "sys.argv",
            [
                "inventory",
                "--expected-host",
                "approved-host",
                "--output",
                "/tmp/not-written",
            ],
        ), patch.object(
            inventory.socket, "gethostname", return_value="approved-host"
        ), patch.object(
            inventory.Path, "is_socket", return_value=True
        ), patch.object(
            inventory, "run", return_value=json.dumps({"Name": "other-host"})
        ) as runner:
            with self.assertRaises(SystemExit):
                inventory.main()
            self.assertEqual(runner.call_count, 1)
            self.assertEqual(runner.call_args.args[0][1], "info")


if __name__ == "__main__":
    unittest.main()

"""Subprocess tests: fake Docker/timeout/sleep; never invoke a daemon/network."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).with_name('gitleaks-history.sh').resolve()
IMAGE = 'ghcr.io/gitleaks/gitleaks:v8.30.0@sha256:691af3c7c5a48b16f187ce3446d5f194838f91238f27270ed36eef6359a574d9'
MOCK = r'''#!/usr/bin/env python3
import json, os, pathlib, subprocess, sys
kind = pathlib.Path(sys.argv[0]).name
root = pathlib.Path(os.environ['MOCK_ROOT'])
with (root / 'calls').open('a') as f:
    f.write(json.dumps([kind, *sys.argv[1:]]) + '\n')
if kind == 'sleep':
    sys.exit(0)
if kind == 'timeout':
    if 'MOCK_TIMEOUT_EXIT' in os.environ:
        sys.exit(int(os.environ['MOCK_TIMEOUT_EXIT']))
    sys.exit(subprocess.run(sys.argv[4:]).returncode)
if sys.argv[1] == 'pull':
    state = root / 'count'
    index = int(state.read_text()) if state.exists() else 0
    state.write_text(str(index + 1))
    codes = json.loads(os.environ['MOCK_PULL_CODES'])
    # Deliberately split signed URL/token content over both streams.
    print('https://blob.invalid/object?sig=NEVER_LOG_QUERY&token=NEVER_LOG_TOKEN')
    print('Authorization: Bearer NEVER_LOG_BEARER\n&sig=NEVER_LOG_FRAGMENT', file=sys.stderr)
    sys.exit(codes[min(index, len(codes) - 1)])
if sys.argv[1] == 'run':
    print('scanner-redacted-output')
    sys.exit(int(os.environ.get('MOCK_SCAN_CODE', '0')))
raise SystemExit(99)
'''


class PullTests(unittest.TestCase):
    def exercise(self, pulls, scan=0, timeout=None):
        with tempfile.TemporaryDirectory(prefix='gitleaks-pull-test-') as temp:
            root = Path(temp)
            for tool in ('docker', 'timeout', 'sleep'):
                path = root / tool
                path.write_text(MOCK)
                path.chmod(0o700)
            env = {**os.environ, 'PATH': temp + os.pathsep + os.environ['PATH'],
                   'MOCK_ROOT': temp, 'MOCK_PULL_CODES': json.dumps(pulls), 'MOCK_SCAN_CODE': str(scan)}
            if timeout is not None:
                env['MOCK_TIMEOUT_EXIT'] = str(timeout)
            else:
                env.pop('MOCK_TIMEOUT_EXIT', None)
            result = subprocess.run(['bash', str(SCRIPT)], cwd=root, env=env,
                                    capture_output=True, text=True, timeout=10)
            calls = [json.loads(line) for line in (root / 'calls').read_text().splitlines()]
            return result, calls, str(root.resolve())

    def assert_scan(self, calls, cwd):
        scans = [c for c in calls if c[:2] == ['docker', 'run']]
        self.assertEqual(scans, [['docker', 'run', '--rm', '--pull=never', '-v', cwd + ':/repo:ro',
                                 IMAGE, 'git', '/repo', '--redact', '--no-banner']])

    def test_pull_error_then_success_scans_once(self):
        result, calls, cwd = self.exercise([1, 0])
        self.assertEqual(result.returncode, 0)
        self.assertEqual(len([c for c in calls if c[:2] == ['docker', 'pull']]), 2)
        self.assertEqual([c for c in calls if c[0] == 'sleep'], [['sleep', '5']])
        self.assert_scan(calls, cwd)

    def test_first_pull_success_no_backoff(self):
        result, calls, cwd = self.exercise([0])
        self.assertEqual(result.returncode, 0)
        self.assert_scan(calls, cwd)
        self.assertFalse(any(c[0] == 'sleep' for c in calls))

    def test_three_errors_no_scan_propagates_final_error(self):
        result, calls, _ = self.exercise([1, 2, 17])
        self.assertEqual(result.returncode, 17)
        self.assertEqual(len([c for c in calls if c[:2] == ['docker', 'pull']]), 3)
        self.assertFalse(any(c[:2] == ['docker', 'run'] for c in calls))
        self.assertEqual([c for c in calls if c[0] == 'sleep'], [['sleep', '5'], ['sleep', '10']])

    def test_findings_and_operational_failure_never_retry(self):
        for code in (1, 125):
            with self.subTest(code=code):
                result, calls, cwd = self.exercise([0], code)
                self.assertEqual(result.returncode, code)
                self.assert_scan(calls, cwd)
                self.assertEqual(len([c for c in calls if c[:2] == ['docker', 'pull']]), 1)
                self.assertFalse(any(c[0] == 'sleep' for c in calls))

    def test_timeouts_bound_three_attempts_without_scanning(self):
        result, calls, _ = self.exercise([0], timeout=124)
        self.assertEqual(result.returncode, 124)
        timers = [c for c in calls if c[0] == 'timeout']
        self.assertEqual(timers, [['timeout', '--signal=TERM', '--kill-after=5s', '120s',
                                   'docker', 'pull', IMAGE]] * 3)
        self.assertFalse(any(c[:2] == ['docker', 'run'] for c in calls))

    def test_pull_output_never_leaks_queries_tokens_or_headers(self):
        result, _, _ = self.exercise([1, 0])
        for secret in ('NEVER_LOG', 'blob.invalid', 'Authorization', 'sig=', 'token='):
            self.assertNotIn(secret, result.stdout + result.stderr)
        self.assertIn('scanner-redacted-output', result.stdout)

    def test_immutable_image_for_every_pull_and_scan(self):
        result, calls, cwd = self.exercise([1, 1, 0])
        self.assertEqual(result.returncode, 0)
        self.assertEqual([c for c in calls if c[:2] == ['docker', 'pull']],
                         [['docker', 'pull', IMAGE]] * 3)
        self.assert_scan(calls, cwd)

    def test_pull_cancellation_no_retry_or_scan(self):
        for code in (130, 137, 143):
            with self.subTest(code=code):
                result, calls, _ = self.exercise([code, 0])
                self.assertEqual(result.returncode, code)
                self.assertEqual(len([c for c in calls if c[:2] == ['docker', 'pull']]), 1)
                self.assertFalse(any(c[0] == 'sleep' or c[:2] == ['docker', 'run'] for c in calls))


if __name__ == '__main__':
    unittest.main()

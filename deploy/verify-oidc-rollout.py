#!/usr/bin/env python3
"""Minimal rollout gate for an OIDC-only API replica."""

import json
import sys
import urllib.error
import urllib.request


def request(base: str, path: str, method: str = "GET", body: bytes | None = None) -> int:
    req = urllib.request.Request(
        base + path,
        data=body,
        method=method,
        headers={"Content-Type": "application/json", "Origin": "https://sentinelops.tqi.com.br"},
    )
    try:
        with urllib.request.urlopen(req, timeout=15) as response:
            return response.status
    except urllib.error.HTTPError as exc:
        return exc.code


def main() -> int:
    if len(sys.argv) != 2:
        raise SystemExit("usage: verify-oidc-rollout.py API_IP")
    base = f"http://{sys.argv[1]}:8080"
    checks = {
        "healthz": request(base, "/healthz"),
        "readyz": request(base, "/readyz"),
        "local_login_disabled": request(base, "/api/v1/auth/login", "POST", b"{}"),
        "catalog_requires_oidc": request(base, "/api/v1/observability/explorer/catalog"),
    }
    expected = {"healthz": 200, "readyz": 200, "local_login_disabled": 404, "catalog_requires_oidc": 401}
    failures = {name: (actual, expected[name]) for name, actual in checks.items() if actual != expected[name]}
    print(json.dumps({"checks": checks, "failures": failures}, sort_keys=True))
    return 1 if failures else 0


if __name__ == "__main__":
    raise SystemExit(main())

#!/usr/bin/env python3
"""PUT de capability local; confirmação por hash/tamanho/identidade, sem secrets em logs."""
import hashlib
import json
import os
from pathlib import Path
import sys
import urllib.request


def verify(receipt, data, name, identity):
    expected = {"name": name, "bytes": len(data), "sha256": hashlib.sha256(data).hexdigest(), **identity}
    if any(receipt.get(key) != value for key, value in expected.items()):
        raise ValueError("artifact receipt mismatch")


def main():
    path = Path(sys.argv[1])
    if path.is_symlink() or path.name != "image-alloy.json":
        raise ValueError("unexpected artifact")
    data = path.read_bytes()
    if not 0 < len(data) <= 25 * 1024 * 1024:
        raise ValueError("artifact size exceeds local quota")
    url = os.environ["LOCAL_ARTIFACT_URL"]
    if url != "http://192.168.5.101:3129":
        raise ValueError("unexpected local artifact endpoint")
    identity = {"repository": os.environ["GITHUB_REPOSITORY"],
                "run": int(os.environ["GITHUB_RUN_ID"]),
                "sourceSha": os.environ["ARTIFACT_SOURCE_SHA"]}
    request = urllib.request.Request(url + "/artifacts/" + path.name, data=data, method="PUT",
                                     headers={"Authorization": "Bearer " + os.environ["LOCAL_ARTIFACT_TOKEN"],
                                              "Content-Type": "application/json"})
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    with opener.open(request, timeout=120) as response:
        receipt = json.loads(response.read(4097))
    verify(receipt, data, path.name, identity)
    print(json.dumps({"artifact": path.name, "sha256": receipt["sha256"], "bytes": receipt["bytes"], "verified": True}))


if __name__ == "__main__":
    main()

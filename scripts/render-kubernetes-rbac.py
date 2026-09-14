#!/usr/bin/env python3
"""Render RoleBindings only for explicit Kubernetes inventory namespaces."""

import argparse
import json
import re
import sys
from pathlib import Path

NAME = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$")


def fail(message: str) -> None:
    raise ValueError(f"[sentinelops][kubernetes-collector] {message}")


def load(path: Path) -> dict:
    try:
        document = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise ValueError(f"[sentinelops][kubernetes-collector] scope inválido: {exc}") from exc
    required = {"context", "namespaces", "environment", "ownerTeam"}
    if not isinstance(document, dict) or set(document) != required:
        fail("scope deve conter exatamente context, namespaces, environment e ownerTeam")
    namespaces = document["namespaces"]
    if not isinstance(namespaces, list) or not namespaces or len(namespaces) > 100:
        fail("namespaces deve conter entre 1 e 100 entradas")
    result: list[str] = []
    for namespace in namespaces:
        if not isinstance(namespace, str) or not NAME.fullmatch(namespace) or namespace in result:
            fail("namespace deve ser DNS-safe e único")
        result.append(namespace)
    return {"namespaces": sorted(result)}


def render(scope: dict) -> str:
    documents = []
    for namespace in scope["namespaces"]:
        documents.append(
            "\n".join(
                [
                    "apiVersion: rbac.authorization.k8s.io/v1",
                    "kind: RoleBinding",
                    "metadata:",
                    f"  name: sentinelops-kubernetes-observer-{namespace}",
                    f"  namespace: {namespace}",
                    "subjects:",
                    "  - kind: ServiceAccount",
                    "    name: sentinelops-kubernetes-observer",
                    "    namespace: sentinelops-observability",
                    "roleRef:",
                    "  apiGroup: rbac.authorization.k8s.io",
                    "  kind: ClusterRole",
                    "  name: sentinelops-kubernetes-observer-namespaced",
                ]
            )
        )
    return "\n---\n".join(documents) + "\n"


def main() -> int:
    parser = argparse.ArgumentParser(description="renderiza RoleBindings Kubernetes de escopo fechado")
    parser.add_argument("--scope", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    try:
        payload = render(load(args.scope))
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(payload, encoding="utf-8")
    except ValueError as exc:
        print(exc, file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

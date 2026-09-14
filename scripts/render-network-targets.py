#!/usr/bin/env python3
"""Render a closed, exact-IP SNMP allowlist into an Alloy discovery block."""

import argparse
import ipaddress
import json
import re
import sys
from pathlib import Path

NAME = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$")
ALLOWED_MODULES = {"if_mib", "system", "hrSystem"}
REQUIRED_FIELDS = {"asset_id", "address", "site", "environment", "team", "module"}


def fail(message: str) -> None:
    raise ValueError(f"[sentinelops][network-collector] {message}")


def field(value: object, name: str) -> str:
    if not isinstance(value, str) or not NAME.fullmatch(value):
        fail(f"{name} inválido; use texto DNS-safe de até 63 caracteres")
    return value


def target_address(value: object) -> str:
    if not isinstance(value, str):
        fail("address deve ser IP literal, não hostname, URL ou CIDR")
    try:
        address = ipaddress.ip_address(value)
    except ValueError as exc:
        raise ValueError("[sentinelops][network-collector] address deve ser IP literal, não hostname, URL ou CIDR") from exc
    if address.version == 6:
        return f"udp://[{address.compressed}]:161"
    return f"udp://{address.compressed}:161"


def quote(value: str) -> str:
    return json.dumps(value, ensure_ascii=False)


def render(source: Path) -> str:
    try:
        document = json.loads(source.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise ValueError(f"[sentinelops][network-collector] targets.json inválido: {exc}") from exc
    if not isinstance(document, dict) or set(document) != {"targets"} or not isinstance(document["targets"], list):
        fail("manifesto deve conter somente targets como lista")
    if not document["targets"]:
        fail("manifesto não pode ter allowlist vazia")
    rendered: list[str] = []
    seen_assets: set[str] = set()
    seen_addresses: set[str] = set()
    for index, item in enumerate(document["targets"], start=1):
        if not isinstance(item, dict) or set(item) != REQUIRED_FIELDS:
            fail(f"target {index} deve conter exatamente {', '.join(sorted(REQUIRED_FIELDS))}")
        asset_id = field(item["asset_id"], "asset_id")
        site = field(item["site"], "site")
        environment = field(item["environment"], "environment")
        team = field(item["team"], "team")
        module = item["module"]
        if module not in ALLOWED_MODULES:
            fail(f"módulo {module!r} não permitido; use {', '.join(sorted(ALLOWED_MODULES))}")
        address = target_address(item["address"])
        if asset_id in seen_assets:
            fail(f"asset_id duplicado: {asset_id}")
        if address in seen_addresses:
            fail(f"address duplicado: {address}")
        seen_assets.add(asset_id)
        seen_addresses.add(address)
        labels = {
            "__address__": "snmp-exporter:9116",
            "__param_auth": "sentinelops_v3_authpriv",
            "__param_module": module,
            "__param_target": address,
            "asset_id": asset_id,
            "deployment_environment": environment,
            "job": "network-snmp",
            "site": site,
            "team": team,
        }
        lines = ["  {"] + [f"    {key} = {quote(value)}," for key, value in labels.items()] + ["  },"]
        rendered.append("\n".join(lines))
    return "// Gerado por scripts/render-network-targets.py; não editar.\n\ndiscovery.static \"network_devices\" {\n  targets = [\n" + "\n".join(rendered) + "\n  ]\n}\n"


def main() -> int:
    parser = argparse.ArgumentParser(description="renderiza a allowlist SNMP para Alloy")
    parser.add_argument("--targets", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    try:
        payload = render(args.targets)
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(payload, encoding="utf-8")
    except ValueError as exc:
        print(exc, file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

#!/usr/bin/env python3
"""Validate the JSON-compatible YAML spec index without external tooling."""
import json
from pathlib import Path
import re
import sys

root = Path(__file__).resolve().parents[2]
index_path = root / "specs" / "index.yaml"
try:
    index = json.loads(index_path.read_text())
except (OSError, json.JSONDecodeError) as exc:
    print(f"FAIL: cannot parse {index_path}: {exc}", file=sys.stderr)
    raise SystemExit(1)

specs = index.get("specs", [])
ids = [item.get("id") for item in specs]
errors = []
if len(ids) != len(set(ids)) or any(not isinstance(value, str) or not value.startswith("SPEC-") for value in ids):
    errors.append("IDs de spec devem ser únicos e no formato SPEC-###")
expected_ids = {f"SPEC-{number:03d}" for number in range(1, 21)}
if set(ids) != expected_ids:
    missing = sorted(expected_ids - set(ids))
    unexpected = sorted(set(ids) - expected_ids)
    if missing:
        errors.append(f"catálogo obrigatório incompleto: {', '.join(missing)}")
    if unexpected:
        errors.append(f"IDs fora do catálogo obrigatório: {', '.join(unexpected)}")
known = set(ids)
allowed_states = {"PLANNED", "IN_PROGRESS", "VALIDATED_LOCAL", "VALIDATED_TARGET", "OPERATIONAL", "DONE"}
for item in specs:
    path = root / item.get("path", "")
    if not path.is_file():
        errors.append(f"spec ausente: {item.get('path')}")
        continue
    if not item.get("acceptance"):
        errors.append(f"{item.get('id')} não possui critérios de aceite")
    if item.get("state") not in allowed_states:
        errors.append(f"{item.get('id')} possui estado inválido")
    if item.get("state") == "DONE" and not item.get("evidence"):
        errors.append(f"{item.get('id')} está DONE sem evidência")
    content = path.read_text(encoding="utf-8")
    if not content.startswith(f"# {item.get('id')}"):
        errors.append(f"{item.get('id')} não possui título compatível")
    if "## Critérios" not in content:
        errors.append(f"{item.get('id')} não possui seção Critérios")
    for acceptance in item.get("acceptance", []):
        if acceptance not in content:
            errors.append(f"{item.get('id')} não declara {acceptance} no arquivo")
    if item.get("state") in {"VALIDATED_LOCAL", "VALIDATED_TARGET", "OPERATIONAL", "DONE"} and re.search(r"a-implementar|a implementar", content, re.IGNORECASE):
        errors.append(f"{item.get('id')} validada contém referência pendente")
    for dependency in item.get("dependsOn", []):
        if dependency not in known:
            errors.append(f"{item.get('id')} depende de spec inexistente {dependency}")

graph = {item["id"]: item.get("dependsOn", []) for item in specs if item.get("id")}
visiting, visited = set(), set()
def visit(node):
    if node in visiting:
        errors.append(f"ciclo de dependência detectado em {node}")
        return
    if node in visited:
        return
    visiting.add(node)
    for dependency in graph.get(node, []):
        visit(dependency)
    visiting.remove(node)
    visited.add(node)
for spec_id in graph:
    visit(spec_id)

if errors:
    print("FAIL: validação de specs", file=sys.stderr)
    print("\n".join(f"- {error}" for error in errors), file=sys.stderr)
    raise SystemExit(1)
print(f"PASS: {len(specs)} specs válidas; nenhuma spec DONE sem evidência")

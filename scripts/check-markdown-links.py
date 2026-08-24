#!/usr/bin/env python3
"""Fail when a local Markdown link points to a missing repository path."""

from __future__ import annotations

import re
import sys
from pathlib import Path
from urllib.parse import unquote, urlsplit


REPOSITORY = Path(__file__).resolve().parent.parent
LINK = re.compile(r"!?\[[^\]]*\]\(([^)]+)\)")
SCHEMES = {"http", "https", "mailto", "tel", "data"}
EXCLUDED_DIRECTORIES = {".git", ".terraform", "node_modules", "artifacts"}


def markdown_files() -> list[Path]:
    return sorted(
        path
        for path in REPOSITORY.rglob("*.md")
        if not EXCLUDED_DIRECTORIES.intersection(path.parts) and path.is_file()
    )


def link_target(raw: str) -> str:
    raw = raw.strip()
    if raw.startswith("<") and ">" in raw:
        return raw[1 : raw.index(">")]
    return raw.split(maxsplit=1)[0]


def main() -> int:
    failures: list[str] = []
    checked = 0

    for document in markdown_files():
        in_fence = False
        for line_number, line in enumerate(document.read_text(encoding="utf-8").splitlines(), 1):
            if line.lstrip().startswith("```"):
                in_fence = not in_fence
                continue
            if in_fence:
                continue

            for match in LINK.finditer(line):
                target = link_target(match.group(1))
                parsed = urlsplit(target)
                if not target or target.startswith("#") or parsed.scheme.lower() in SCHEMES:
                    continue

                local_path = unquote(parsed.path)
                if not local_path:
                    continue
                resolved = (
                    REPOSITORY / local_path.lstrip("/")
                    if local_path.startswith("/")
                    else document.parent / local_path
                ).resolve()
                checked += 1
                if not resolved.is_relative_to(REPOSITORY) or not resolved.exists():
                    relative_document = document.relative_to(REPOSITORY)
                    failures.append(f"{relative_document}:{line_number}: {target}")

    if failures:
        print("Broken local Markdown links:", file=sys.stderr)
        print("\n".join(failures), file=sys.stderr)
        return 1

    print(f"Markdown links OK: {checked} local targets checked")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

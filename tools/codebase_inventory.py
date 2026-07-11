#!/usr/bin/env python3
"""Create a deterministic, line-level inventory of the LightBridge repository.

The inventory is intentionally content-addressed. It gives reviewers a repeatable
way to prove which text files were included in an audit without treating generated
code, lock files, or build output as hand-maintained architecture.
"""
from __future__ import annotations

import argparse
import hashlib
import os
from dataclasses import dataclass
from pathlib import Path
import sys
from typing import Iterable

EXCLUDED_DIRS = {
    ".git", ".idea", ".vscode", ".pnpm-store", "node_modules", "dist",
    "build", "coverage", "tmp", ".cache",
}
BINARY_EXTENSIONS = {
    ".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".woff", ".woff2",
    ".ttf", ".otf", ".zip", ".gz", ".tar", ".7z", ".pdf", ".exe", ".dll",
    ".dylib", ".so", ".a", ".o", ".wasm",
}
LOCK_NAMES = {"pnpm-lock.yaml", "package-lock.json", "yarn.lock", "go.sum"}
GENERATED_PATH_PARTS = {
    "ent", "generated", "mocks", "mock", "openapi-generated",
}

@dataclass(frozen=True)
class Entry:
    path: str
    category: str
    ownership: str
    lines: int
    bytes: int
    sha256: str


def classify(path: Path) -> str:
    p = path.as_posix()
    suffix = path.suffix.lower()
    if p.startswith("backend/") and suffix == ".go":
        return "backend-go"
    if p.startswith("frontend/") and suffix == ".vue":
        return "frontend-vue"
    if p.startswith("frontend/") and suffix in {".ts", ".tsx", ".js", ".jsx"}:
        return "frontend-script"
    if "/migrations/" in p and suffix == ".sql":
        return "migration"
    if p.startswith(".github/workflows/"):
        return "workflow"
    if p.startswith("docs/") or suffix == ".md":
        return "documentation"
    if suffix in {".yaml", ".yml", ".json", ".toml", ".ini", ".env", ".example"}:
        return "configuration"
    if suffix in {".sh", ".bash", ".py"} or path.name in {"Makefile", "Dockerfile"}:
        return "tooling"
    return "other-text"


def ownership(path: Path, data: bytes) -> str:
    if path.name in LOCK_NAMES:
        return "lockfile"
    parts = set(path.parts)
    prefix = data[:4096].decode("utf-8", errors="ignore").lower()
    generated_marker = (
        "code generated" in prefix
        or "do not edit" in prefix
        or "automatically generated" in prefix
        or "auto-generated" in prefix
    )
    if generated_marker:
        return "generated"
    if "backend" in parts and "internal" in parts and "ent" in parts:
        return "generated"
    if parts.intersection(GENERATED_PATH_PARTS) and generated_marker:
        return "generated"
    return "maintained"


def is_probably_text(path: Path, data: bytes) -> bool:
    if path.suffix.lower() in BINARY_EXTENSIONS:
        return False
    if b"\x00" in data[:8192]:
        return False
    return True


def iter_files(root: Path) -> Iterable[Path]:
    for current, dirs, files in os.walk(root):
        dirs[:] = sorted(d for d in dirs if d not in EXCLUDED_DIRS)
        base = Path(current)
        for name in sorted(files):
            path = base / name
            if path.is_symlink():
                continue
            yield path


def collect(root: Path, output: Path) -> list[Entry]:
    entries: list[Entry] = []
    output_abs = output.resolve()
    for path in iter_files(root):
        if path.resolve() == output_abs:
            continue
        rel = path.relative_to(root)
        data = path.read_bytes()
        if not is_probably_text(rel, data):
            continue
        entries.append(
            Entry(
                path=rel.as_posix(),
                category=classify(rel),
                ownership=ownership(rel, data),
                lines=len(data.splitlines()),
                bytes=len(data),
                sha256=hashlib.sha256(data).hexdigest(),
            )
        )
    return entries


def render(entries: list[Entry]) -> str:
    rows = ["path\tcategory\townership\tlines\tbytes\tsha256"]
    rows.extend(
        f"{e.path}\t{e.category}\t{e.ownership}\t{e.lines}\t{e.bytes}\t{e.sha256}"
        for e in entries
    )
    return "\n".join(rows) + "\n"


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", default=".")
    parser.add_argument("--output", default="docs/architecture/CODEBASE_INVENTORY.tsv")
    parser.add_argument("--check", action="store_true", help="fail when the checked-in inventory is stale")
    args = parser.parse_args()

    root = Path(args.root).resolve()
    output = (root / args.output).resolve()
    entries = collect(root, output)
    content = render(entries)

    if args.check:
        if not output.exists() or output.read_text(encoding="utf-8") != content:
            print(f"stale codebase inventory: {output.relative_to(root)}", file=sys.stderr)
            return 1
    else:
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_text(content, encoding="utf-8")

    maintained = sum(e.lines for e in entries if e.ownership == "maintained")
    generated = sum(e.lines for e in entries if e.ownership == "generated")
    print(
        f"indexed {len(entries)} text files; maintained_lines={maintained}; "
        f"generated_lines={generated}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

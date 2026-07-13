#!/usr/bin/env python3
"""Static invariants for LightBridge release configuration.

This intentionally uses only the Python standard library so it can run on a
fresh GitHub-hosted runner before dependencies are installed. GitHub validates
workflow YAML syntax while loading the workflow; GoReleaser validates its own
schema during the release step. This script protects the cross-file invariants
that those individual parsers cannot express.
"""

from __future__ import annotations

from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parents[1]
WORKFLOW = ROOT / ".github/workflows/release.yml"
FULL = ROOT / ".goreleaser.yaml"
SIMPLE = ROOT / ".goreleaser.simple.yaml"
GO_MOD = ROOT / "backend/go.mod"
GO_VERSION_FILES = (
    ROOT / "Dockerfile",
    ROOT / "backend/Dockerfile",
    ROOT / "deploy/Dockerfile",
    ROOT / ".github/workflows/backend-ci.yml",
    ROOT / ".github/workflows/security-scan.yml",
)


def fail(message: str) -> None:
    print(f"release configuration error: {message}", file=sys.stderr)
    raise SystemExit(1)


def require(text: str, needle: str, source: Path) -> None:
    if needle not in text:
        fail(f"{source.relative_to(ROOT)} is missing required invariant: {needle}")


def forbid(text: str, needle: str, source: Path) -> None:
    if needle in text:
        fail(f"{source.relative_to(ROOT)} contains forbidden release behavior: {needle}")



_ACTION_REF_RE = re.compile(r"^\s*uses:\s*([^\s#]+)", re.MULTILINE)
_SHA_PIN_RE = re.compile(r"^[^@]+@[0-9a-f]{40}$")


def require_actions_pinned(workflow: str) -> None:
    refs = [match.group(1) for match in _ACTION_REF_RE.finditer(workflow)]
    unpinned = [ref for ref in refs if not ref.startswith("./") and not _SHA_PIN_RE.fullmatch(ref)]
    if unpinned:
        fail("release workflow contains mutable Action refs: " + ", ".join(sorted(set(unpinned))))


def require_minimal_permissions(workflow: str) -> None:
    require(workflow, "permissions:\n  contents: read", WORKFLOW)
    allowed_write_jobs = {"release", "publish-preview-release", "sync-version-file"}
    current_job: str | None = None
    for line in workflow.splitlines():
        match = re.match(r"^  ([A-Za-z0-9_-]+):$", line)
        if match:
            current_job = match.group(1)
        if re.match(r"^      (contents|packages): write$", line):
            if current_job not in allowed_write_jobs:
                fail(f"write permission granted to unexpected job: {current_job}")


_KEY_RE = re.compile(r"^(?P<key>[A-Za-z0-9_.-]+):(?P<value>.*)$")


def forbid_duplicate_mapping_keys(source: Path, text: str) -> None:
    """Reject duplicate YAML mapping keys without adding a YAML dependency.

    Common YAML loaders silently keep the last duplicate value, while GitHub's
    workflow parser may reject the file. This indentation-aware scanner covers
    the block mappings and sequence-of-mappings used by the release files and
    intentionally ignores block scalar bodies.
    """

    scopes: dict[int, dict[str, int]] = {}
    block_scalar_indent: int | None = None

    for line_number, raw_line in enumerate(text.splitlines(), start=1):
        if not raw_line.strip() or raw_line.lstrip().startswith("#"):
            continue

        indent = len(raw_line) - len(raw_line.lstrip(" "))
        if block_scalar_indent is not None:
            if indent > block_scalar_indent:
                continue
            block_scalar_indent = None

        content = raw_line.lstrip(" ")
        sequence_item = content.startswith("- ")
        if sequence_item:
            content = content[2:].lstrip(" ")
            mapping_indent = indent + 2
            for level in [level for level in scopes if level >= mapping_indent]:
                del scopes[level]
        else:
            mapping_indent = indent
            for level in [level for level in scopes if level > mapping_indent]:
                del scopes[level]

        match = _KEY_RE.match(content)
        if not match:
            continue

        key = match.group("key")
        scope = scopes.setdefault(mapping_indent, {})
        if key in scope:
            fail(
                f"{source.relative_to(ROOT)} has duplicate YAML key {key!r} "
                f"at lines {scope[key]} and {line_number}"
            )
        scope[key] = line_number

        value = match.group("value").strip()
        if value in {"|", ">", "|-", ">-", "|+", ">+"}:
            block_scalar_indent = mapping_indent


def require_consistent_go_version(workflow: str) -> None:
    go_mod = GO_MOD.read_text(encoding="utf-8")
    match = re.search(r"^go\s+(\d+\.\d+\.\d+)\s*$", go_mod, re.MULTILINE)
    if not match:
        fail("backend/go.mod must declare an exact Go patch version")
    version = match.group(1)
    require(workflow, f"GO_VERSION_EXPECTED: '{version}'", WORKFLOW)
    for source in GO_VERSION_FILES:
        text = source.read_text(encoding="utf-8")
        if version not in text:
            fail(f"{source.relative_to(ROOT)} is not pinned to Go {version}")


def main() -> None:
    workflow = WORKFLOW.read_text(encoding="utf-8")
    full = FULL.read_text(encoding="utf-8")
    simple = SIMPLE.read_text(encoding="utf-8")

    require_actions_pinned(workflow)
    require_minimal_permissions(workflow)
    require_consistent_go_version(workflow)

    for source, text in ((WORKFLOW, workflow), (FULL, full), (SIMPLE, simple)):
        forbid_duplicate_mapping_keys(source, text)
        if "\t" in text:
            fail(f"{source.relative_to(ROOT)} contains tab indentation")
        if not text.endswith("\n"):
            fail(f"{source.relative_to(ROOT)} must end with a newline")

    for needle in (
        "tag_sha: ${{ steps.meta.outputs.tag_sha }}",
        "ref: ${{ needs.release-meta.outputs.tag_name }}",
        "go mod verify",
        "python3 tools/codebase_inventory.py --check",
        "make secret-scan",
        "make -C backend test-unit",
        "make -C backend test-integration",
        "make test-frontend",
        "git diff --exit-code -- backend/go.mod backend/go.sum",
        "npm install --global \"corepack@${COREPACK_VERSION}\"",
        "corepack prepare \"pnpm@${PNPM_VERSION}\" --activate",
        "COREPACK_VERSION: '0.31.0'",
        "PNPM_VERSION: '9.15.9'",
        "NODE_VERSION: '22.13.1'",
        "pnpm install --frozen-lockfile",
        "if-no-files-found: error",
        "args: release --clean --config=.goreleaser.yaml",
        "args: release --clean --config=.goreleaser.simple.yaml",
        "DOCKERHUB_PUSH_ENABLED=true",
        "Sync VERSION file with bounded retry",
    ):
        require(workflow, needle, WORKFLOW)

    for needle in (
        "--skip=validate",
        "go mod tidy",
        "github.event.inputs.tag || github.ref",
        "pnpm/action-setup",
    ):
        forbid(workflow, needle, WORKFLOW)

    for source, text in ((FULL, full), (SIMPLE, simple)):
        require(text, "version: 2", source)
        require(text, "-tags=embed", source)
        forbid(text, "go mod tidy", source)
        forbid(text, "before:", source)

    require(full, "goos:\n      - linux\n      - windows\n      - darwin", FULL)
    require(full, "goarch:\n      - amd64\n      - arm64", FULL)
    require(simple, "goos:\n      - linux", SIMPLE)
    require(simple, "goarch:\n      - amd64", SIMPLE)
    require(simple, "skip_upload: true", SIMPLE)

    print("Release configuration invariants passed.")


if __name__ == "__main__":
    main()

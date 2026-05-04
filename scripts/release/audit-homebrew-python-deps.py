#!/usr/bin/env python3
"""Audit the packaged Python dependency set used by the Homebrew formula.

Run this from a virtualenv that has the Homebrew wheelhouse requirements
installed. The audit keeps the runtime roots explicit and verifies that every
installed wheelhouse package is either one of those roots or reachable from
one through package metadata.
"""

from __future__ import annotations

import argparse
import importlib.metadata as metadata
import re
import sys
from collections import deque
from pathlib import Path


ROOT_PACKAGES = {
    # Host-managed infra services.
    "aiohttp": "comms and knowledge HTTP servers",
    "httpx": "gateway, embedding, synthesis, and comms HTTP clients",
    "pydantic": "comms message models",
    "pyyaml": "egress and knowledge YAML configuration",
    "requests": "credential swap token exchange handlers",

    # Egress proxy runtime.
    "mitmproxy": "egress proxy addon host",
    "mitmproxy-macos": "mitmproxy macOS support package",

    # Enabled optional capabilities.
    "networkx": "knowledge graph analysis when available",
    "pyjwt": "GitHub App credential swap handler when configured",
    "sqlite-vec": "knowledge vector search when available",
}

KNOWN_OPTIONAL_IMPORTS = {
    "fitz": "PyMuPDF PDF extraction; extractor degrades when unavailable",
    "watchdog": "real-time knowledge watcher; polling remains available",
}


REQ_NAME_RE = re.compile(r"^\s*([A-Za-z0-9_.-]+)")


def normalize(name: str) -> str:
    return re.sub(r"[-_.]+", "-", name).lower()


def requirement_name(line: str) -> str | None:
    line = line.strip()
    if not line or line.startswith("#"):
        return None
    match = REQ_NAME_RE.match(line)
    if not match:
        return None
    return normalize(match.group(1))


def dependency_name(requirement: str) -> str | None:
    match = REQ_NAME_RE.match(requirement)
    if not match:
        return None
    return normalize(match.group(1))


def load_requirements(path: Path) -> set[str]:
    return {
        name
        for line in path.read_text(encoding="utf-8").splitlines()
        if (name := requirement_name(line))
    }


def installed_distributions() -> dict[str, metadata.Distribution]:
    return {normalize(dist.metadata["Name"]): dist for dist in metadata.distributions()}


def dependency_closure(roots: set[str], installed: dict[str, metadata.Distribution]) -> set[str]:
    seen: set[str] = set()
    queue: deque[str] = deque(sorted(roots))

    while queue:
        name = queue.popleft()
        if name in seen:
            continue
        seen.add(name)

        dist = installed.get(name)
        if dist is None:
            continue

        for req in dist.requires or []:
            dep = dependency_name(req)
            if dep and dep in installed and dep not in seen:
                queue.append(dep)

    return seen


def scan_optional_imports(source_root: Path) -> dict[str, str]:
    found: dict[str, str] = {}
    for path in source_root.rglob("*.py"):
        if any(part.startswith(".") for part in path.parts):
            continue
        text = path.read_text(encoding="utf-8")
        for module, note in KNOWN_OPTIONAL_IMPORTS.items():
            if re.search(rf"^\s*(import|from)\s+{re.escape(module)}\b", text, re.MULTILINE):
                found[module] = note
    return found


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--requirements",
        default="scripts/release/homebrew-python-requirements.txt",
        type=Path,
        help="locked Homebrew Python requirements file",
    )
    parser.add_argument(
        "--source-root",
        default="services",
        type=Path,
        help="Python service source root to scan for known optional imports",
    )
    args = parser.parse_args()

    requirements = load_requirements(args.requirements)
    installed = installed_distributions()
    roots = set(ROOT_PACKAGES)

    missing_installed = sorted(requirements - set(installed))
    missing_roots = sorted(roots - set(installed))
    closure = dependency_closure(roots, installed)
    unexplained = sorted(requirements - closure)
    optional_imports = scan_optional_imports(args.source_root)

    print("Homebrew Python dependency audit")
    print(f"requirements: {args.requirements}")
    print(f"locked packages: {len(requirements)}")
    print(f"runtime roots: {len(roots)}")
    print(f"reachable packages: {len(requirements & closure)}")

    if optional_imports:
        print("\nKnown optional imports not packaged as default roots:")
        for module, note in sorted(optional_imports.items()):
            print(f"  - {module}: {note}")

    if missing_installed:
        print("\nPackages in requirements but not installed:")
        for name in missing_installed:
            print(f"  - {name}")

    if missing_roots:
        print("\nRuntime roots not installed:")
        for name in missing_roots:
            print(f"  - {name}: {ROOT_PACKAGES[name]}")

    if unexplained:
        print("\nPackages not explained by runtime roots or dependency metadata:")
        for name in unexplained:
            print(f"  - {name}")

    if missing_installed or missing_roots or unexplained:
        return 1

    print("\nAudit passed.")
    return 0


if __name__ == "__main__":
    sys.exit(main())

#!/usr/bin/env python3
"""Fails on a panic( call in a non-test Go file outside the allowlist.

Library code returns errors (CONTRIBUTING.md, Go checks): a panic in a goroutine the facade
did not start kills the app, and onPanic is the last net, not the first. ALLOWED is the only
exception list: each file may hold at most that many panic( calls, with the reason.

Run: python3 scripts/check-panics.py
"""
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

# Path (relative to ROOT) → how many panic( calls it may hold, and why.
ALLOWED = {
    # Init-time decode of embedded files: a broken one is a build error, and the package's
    # tests load it on every PR.
    "go/plugin/builtin.go": 1,
    "go/ichorgo/appcatalog.go": 1,
    # Fixed literals of the demo inventory, decoded by the tests.
    "go/ichorgo/kube_argocd_network_demo.go": 1,
    # Re-panics after recording a recovered panic (the audit entry, the capture's cleanup).
    "go/ichorgo/audit.go": 1,
    "go/ichorgo/pcapcapture.go": 1,
}

PANIC = re.compile(r"\bpanic\(")


def skipped(rel: str) -> bool:
    parts = rel.split("/")[:-1]
    return rel.endswith("_test.go") or any(p.startswith(".") or p == "build" for p in parts)


def panic_count(path: Path) -> int:
    count = 0
    for line in path.read_text(encoding="utf-8").splitlines():
        code = line.split("//", 1)[0]
        count += len(PANIC.findall(code))
    return count


def offenders(root: Path) -> list[str]:
    """One "path: N panic( (allowed A)" message per file over its allowance, sorted by path."""
    found = []
    base = root / "go"
    if not base.is_dir():
        return found
    for path in sorted(base.rglob("*.go")):
        rel = path.relative_to(root).as_posix()
        if skipped(rel) or not path.is_file():
            continue
        n, allowed = panic_count(path), ALLOWED.get(rel, 0)
        if n > allowed:
            found.append(f"{rel}: {n} panic( (allowed {allowed})")
    return found


def main() -> int:
    problems = offenders(ROOT)
    for p in problems:
        print(p, file=sys.stderr)
    if problems:
        print("return an error instead of panicking (see CONTRIBUTING.md, Go checks)", file=sys.stderr)
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())

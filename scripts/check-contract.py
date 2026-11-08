#!/usr/bin/env python3
"""The facade contract (CYR-440): every golden fixture Go writes into
go/ichorgo/testdata/contract/ must have a row in the app tests that decode it, so a fixture
cannot be added (or renamed) without a model checking it. The Kotlin test is
app/src/test/java/name/levis/ichor/contract/ContractTest.kt ("name" to Row(...)); the Swift one,
when it exists, ios/IchorCore/Tests/IchorCoreTests/ContractTests.swift ("name": ...).
Exits 1 naming each fixture a test does not cover, or a row with no fixture."""

import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
FIXTURES = Path("go/ichorgo/testdata/contract")
TESTS = {
    "Kotlin": (Path("app/src/test/java/name/levis/ichor/contract/ContractTest.kt"), re.compile(r'"([a-z0-9-]+)" to Row\(')),
    "Swift": (Path("ios/IchorCore/Tests/IchorCoreTests/ContractTests.swift"), re.compile(r'^\s*"([a-z0-9-]+)":', re.M)),
}


def problems(root: Path) -> list[str]:
    fixtures = {p.stem for p in (root / FIXTURES).glob("*.json")}
    out = []
    if not fixtures:
        out.append(f"{FIXTURES}: no fixture (run `just contract-update`)")
    for lang, (path, pattern) in TESTS.items():
        test = root / path
        if not test.exists():
            # The Swift test lands after the Kotlin one; a missing Kotlin test is an error.
            if lang == "Kotlin":
                out.append(f"{path}: missing")
            continue
        rows = set(pattern.findall(test.read_text()))
        out += [f"{path}: no row for fixture {name}" for name in sorted(fixtures - rows)]
        out += [f"{path}: row {name} has no fixture in {FIXTURES}" for name in sorted(rows - fixtures)]
    return out


def main() -> int:
    found = problems(ROOT)
    for line in found:
        print(line, file=sys.stderr)
    return 1 if found else 0


if __name__ == "__main__":
    sys.exit(main())

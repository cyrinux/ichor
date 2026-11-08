"""Tests for check-contract.py. Run: python3 -m unittest discover -s scripts -p 'test_*.py'"""

import importlib.util
import os
import tempfile
import unittest
from pathlib import Path

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("check_contract", os.path.join(HERE, "check-contract.py"))
check = importlib.util.module_from_spec(spec)
spec.loader.exec_module(check)

KOTLIN = "app/src/test/java/name/levis/ichor/contract/ContractTest.kt"
SWIFT = "ios/IchorCore/Tests/IchorCoreTests/ContractTests.swift"


class ProblemsTest(unittest.TestCase):
    def setUp(self):
        self._dir = tempfile.TemporaryDirectory()
        self.root = Path(self._dir.name)

    def tearDown(self):
        self._dir.cleanup()

    def write(self, rel: str, text: str = "{}"):
        path = self.root / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)

    def fixtures(self, *names):
        for name in names:
            self.write(f"go/ichorgo/testdata/contract/{name}.json")

    def test_every_fixture_has_a_row(self):
        self.fixtures("alerts", "flux")
        self.write(KOTLIN, '"alerts" to Row(A.serializer()),\n"flux" to Row(F.serializer()),\n')
        self.write(SWIFT, '        "alerts": check(A.self),\n        "flux": check(F.self),\n')
        self.assertEqual(check.problems(self.root), [])

    def test_a_fixture_without_a_row(self):
        self.fixtures("alerts", "flux")
        self.write(KOTLIN, '"alerts" to Row(A.serializer()),\n')
        self.assertEqual(check.problems(self.root), [f"{KOTLIN}: no row for fixture flux"])

    def test_a_row_without_a_fixture(self):
        self.fixtures("alerts")
        self.write(KOTLIN, '"alerts" to Row(A.serializer()),\n"gone" to Row(G.serializer()),\n')
        self.assertEqual(check.problems(self.root), [f"{KOTLIN}: row gone has no fixture in go/ichorgo/testdata/contract"])

    def test_the_swift_test_is_checked_once_it_exists(self):
        self.fixtures("alerts")
        self.write(KOTLIN, '"alerts" to Row(A.serializer()),\n')
        self.assertEqual(check.problems(self.root), [])
        self.write(SWIFT, "// no rows yet\n")
        self.assertEqual(check.problems(self.root), [f"{SWIFT}: no row for fixture alerts"])

    def test_no_fixture_or_no_kotlin_test(self):
        self.assertEqual(
            check.problems(self.root),
            ["go/ichorgo/testdata/contract: no fixture (run `just contract-update`)", f"{KOTLIN}: missing"],
        )


if __name__ == "__main__":
    unittest.main()

"""Tests for check-file-length.py. Run: python3 -m unittest discover -s scripts -p 'test_*.py'"""

import importlib.util
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("check_file_length", os.path.join(HERE, "check-file-length.py"))
check = importlib.util.module_from_spec(spec)
spec.loader.exec_module(check)


class OffendersTest(unittest.TestCase):
    def setUp(self):
        self._dir = tempfile.TemporaryDirectory()
        self.root = Path(self._dir.name)

    def tearDown(self):
        self._dir.cleanup()

    def write(self, rel: str, lines: int):
        path = self.root / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("x\n" * lines)

    def test_file_under_the_limit(self):
        self.write("app/src/main/java/Screen.kt", 800)
        self.assertEqual(check.offenders(self.root), [])

    def test_file_over_the_limit(self):
        self.write("app/src/main/java/Screen.kt", 801)
        self.write("ios/Ichor/Views/Big.swift", 900)
        self.assertEqual(
            check.offenders(self.root),
            [
                "app/src/main/java/Screen.kt: 801 lines (limit 800)",
                "ios/Ichor/Views/Big.swift: 900 lines (limit 800)",
            ],
        )

    def test_tests_get_the_larger_limit(self):
        self.write("go/ichorgo/big_test.go", 1200)
        self.write("ios/IchorCore/Tests/IchorCoreTests/BigTests.swift", 1201)
        self.assertEqual(
            check.offenders(self.root),
            ["ios/IchorCore/Tests/IchorCoreTests/BigTests.swift: 1201 lines (limit 1200)"],
        )

    def test_known_file_at_its_recorded_length(self):
        with mock.patch.dict(check.KNOWN, {"go/ichorgo/known_test.go": 1013}, clear=True):
            self.write("go/ichorgo/known_test.go", 1013)
            self.assertEqual(check.offenders(self.root), [])

    def test_known_file_that_grew(self):
        with mock.patch.dict(check.KNOWN, {"go/ichorgo/known_test.go": 1013}, clear=True):
            self.write("go/ichorgo/known_test.go", 1014)
            self.assertEqual(check.offenders(self.root), ["go/ichorgo/known_test.go: 1014 lines (limit 1013)"])

    def test_build_output_and_generated_files_are_skipped(self):
        self.write("ios/IchorCore/.build/checkouts/dep/Huge.swift", 5000)
        self.write("app/src/main/build/Generated.kt", 5000)
        self.write("go/api/machine.pb.go", 5000)
        self.write("core-widget/src/build/Gen.kt", 5000)
        self.assertEqual(check.offenders(self.root), [])

    def test_unlisted_languages_are_ignored(self):
        self.write("go/notes.txt", 5000)
        self.write("app/src/test/java/BigTest.kt", 5000)
        self.assertEqual(check.offenders(self.root), [])


if __name__ == "__main__":
    unittest.main()

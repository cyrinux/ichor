"""Tests for check-panics.py. Run: python3 -m unittest discover -s scripts -p 'test_*.py'"""

import importlib.util
import os
import tempfile
import unittest
from pathlib import Path

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("check_panics", os.path.join(HERE, "check-panics.py"))
check = importlib.util.module_from_spec(spec)
spec.loader.exec_module(check)


class OffendersTest(unittest.TestCase):
    def setUp(self):
        self._dir = tempfile.TemporaryDirectory()
        self.root = Path(self._dir.name)

    def tearDown(self):
        self._dir.cleanup()

    def write(self, rel: str, text: str):
        path = self.root / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)

    def test_new_panic_fails(self):
        self.write("go/ichorgo/thing.go", 'package ichorgo\n\nfunc f() { panic("boom") }\n')
        self.assertEqual(check.offenders(self.root), ["go/ichorgo/thing.go: 1 panic( (allowed 0)"])

    def test_tests_comments_and_names_pass(self):
        self.write("go/ichorgo/thing_test.go", 'package ichorgo\n\nfunc f() { panic("boom") }\n')
        self.write("go/ichorgo/doc.go", "package ichorgo\n\n// onPanic recovers a panic(r) for the app.\nfunc onPanic() {}\n")
        self.assertEqual(check.offenders(self.root), [])

    def test_allowed_file_may_not_grow(self):
        self.write("go/plugin/builtin.go", 'package plugin\n\nfunc init() { panic("a") }\n')
        self.assertEqual(check.offenders(self.root), [])
        self.write("go/plugin/builtin.go", 'package plugin\n\nfunc init() { panic("a"); panic("b") }\n')
        self.assertEqual(check.offenders(self.root), ["go/plugin/builtin.go: 2 panic( (allowed 1)"])

    def test_repository_passes(self):
        self.assertEqual(check.offenders(Path(HERE).parent), [])


if __name__ == "__main__":
    unittest.main()

"""Tests for changelog.py. Run: python3 -m unittest discover -s scripts -p 'test_*.py'"""

import importlib.util
import os
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("changelog", os.path.join(HERE, "changelog.py"))
changelog = importlib.util.module_from_spec(spec)
spec.loader.exec_module(changelog)


class ScrubTest(unittest.TestCase):
    def test_trailing_reference_in_parentheses(self):
        self.assertEqual(changelog.scrub("Pod logs search (CYR-42)"), "Pod logs search")

    def test_leading_reference(self):
        self.assertEqual(changelog.scrub("CYR-42: pod logs search"), "pod logs search")
        self.assertEqual(changelog.scrub("[CYR-42] pod logs search"), "pod logs search")

    def test_reference_as_scope(self):
        self.assertEqual(changelog.scrub("feat(CYR-42): pod logs search"), "feat: pod logs search")

    def test_several_references(self):
        self.assertEqual(changelog.scrub("Pod logs (CYR-1, CYR-23)"), "Pod logs")
        self.assertEqual(changelog.scrub("Pod logs, refs CYR-1 CYR-2"), "Pod logs")

    def test_lowercase_reference(self):
        self.assertEqual(changelog.scrub("fix cyr-7 crash"), "fix crash")

    def test_text_without_reference_is_unchanged(self):
        self.assertEqual(changelog.scrub("Talos (v1.9) support"), "Talos (v1.9) support")
        self.assertEqual(changelog.scrub("CYRILLIC fonts"), "CYRILLIC fonts")

    def test_classify_drops_reference_from_item(self):
        self.assertEqual(changelog.classify("feat: pod logs search (CYR-42)", ""), ("new", "Pod logs search"))
        self.assertEqual(changelog.classify("fix(CYR-9): crash on resume", ""), ("fixed", "Crash on resume"))


if __name__ == "__main__":
    unittest.main()

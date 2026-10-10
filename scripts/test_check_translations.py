"""Tests for check-translations.py. Run: python3 -m unittest discover -s scripts -p 'test_*.py'"""

import importlib.util
import os
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("check_translations", os.path.join(HERE, "check-translations.py"))
check = importlib.util.module_from_spec(spec)
spec.loader.exec_module(check)


class ConflictMarkerTest(unittest.TestCase):
    def test_every_marker_kind(self):
        text = """<resources>
<<<<<<< HEAD
    <string name="a">A</string>
||||||| a1506f3c
=======
    <string name="b">B</string>
>>>>>>> origin/main
</resources>
"""
        self.assertEqual(check.conflict_markers(text), [2, 4, 5, 7])

    def test_bare_markers(self):
        self.assertEqual(check.conflict_markers("|||||||\n=======\n"), [1, 2])

    def test_text_that_only_looks_like_one(self):
        text = """    <string name="rule">=======</string>
========
"value" : "<<<<<<< not at the start"
"""
        self.assertEqual(check.conflict_markers(text), [])


if __name__ == "__main__":
    unittest.main()

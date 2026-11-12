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


class DuplicateKeyTest(unittest.TestCase):
    def test_a_repeated_key_is_refused_by_name(self):
        text = """{
  "strings" : {
    "Keep" : {
      "localizations" : { "es" : { "stringUnit" : { "state" : "translated", "value" : "Mantener" } } }
    },
    "Keep" : {
      "localizations" : { "es" : { "stringUnit" : { "state" : "translated", "value" : "Conservar" } } }
    }
  }
}"""
        with self.assertRaisesRegex(ValueError, "duplicate key 'Keep'"):
            check.load_catalog(text)

    def test_a_repeated_key_deeper_down_is_refused_too(self):
        text = '{"strings": {"Keep": {"localizations": {"es": {}, "es": {}}}}}'
        with self.assertRaisesRegex(ValueError, "duplicate key 'es'"):
            check.load_catalog(text)

    def test_the_same_key_in_two_objects_passes(self):
        text = '{"strings": {"Keep": {"localizations": {"es": {}}}, "Remove": {"localizations": {"es": {}}}}}'
        self.assertEqual(set(check.load_catalog(text)["strings"]), {"Keep", "Remove"})


class PluralCategoryTest(unittest.TestCase):
    def test_a_language_s_own_categories_pass(self):
        self.assertIsNone(check.plural_problem("ru", {"one", "few", "many", "other"}))
        self.assertIsNone(check.plural_problem("ja", {"other"}))
        self.assertIsNone(check.plural_problem("fr", {"one", "many", "other"}))
        self.assertIsNone(check.plural_problem("fr", {"one", "other"}))

    def test_a_missing_category_fails(self):
        self.assertIn("few", check.plural_problem("ru", {"one", "other"}))

    def test_a_category_the_language_lacks_fails(self):
        self.assertIn("one", check.plural_problem("zh-Hans", {"one", "other"}))
        self.assertIn("many", check.plural_problem("de", {"one", "many", "other"}))

    def test_every_language_has_its_categories(self):
        for lang in ["en"] + check.LANGUAGES:
            self.assertIn(lang, check.PLURALS)


if __name__ == "__main__":
    unittest.main()

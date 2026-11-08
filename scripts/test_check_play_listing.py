"""Tests for check-play-listing.py. Run: python3 -m unittest discover -s scripts -p 'test_*.py'"""

import importlib.util
import os
import tempfile
import unittest
from pathlib import Path

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("check_play_listing", os.path.join(HERE, "check-play-listing.py"))
check = importlib.util.module_from_spec(spec)
spec.loader.exec_module(check)


class OffendersTest(unittest.TestCase):
    def setUp(self):
        self._dir = tempfile.TemporaryDirectory()
        self.root = Path(self._dir.name)

    def tearDown(self):
        self._dir.cleanup()

    def write(self, rel: str, chars: int, char: str = "x"):
        path = self.root / "fastlane/metadata/android" / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(char * chars + "\n", encoding="utf-8")

    def test_texts_at_the_limit(self):
        self.write("en-US/title.txt", 30)
        self.write("en-US/short_description.txt", 80)
        self.write("en-US/full_description.txt", 4000)
        self.write("en-US/changelogs/2084.txt", 500)
        self.assertEqual(check.offenders(self.root), [])

    def test_counts_characters_not_bytes(self):
        self.write("ru-RU/full_description.txt", 4000, "ж")
        self.assertEqual(check.offenders(self.root), [])

    def test_texts_over_the_limit(self):
        self.write("ru-RU/full_description.txt", 4001, "ж")
        self.write("pt-BR/short_description.txt", 81)
        self.write("en-US/changelogs/2084.txt", 501)
        self.assertEqual(
            check.offenders(self.root),
            [
                "fastlane/metadata/android/en-US/changelogs/2084.txt: 501 characters (limit 500)",
                "fastlane/metadata/android/pt-BR/short_description.txt: 81 characters (limit 80)",
                "fastlane/metadata/android/ru-RU/full_description.txt: 4001 characters (limit 4000)",
            ],
        )

    def test_only_the_newest_changelog_counts(self):
        self.write("en-US/changelogs/999.txt", 900)
        self.write("en-US/changelogs/1000.txt", 500)
        self.assertEqual(check.offenders(self.root), [])

    def test_missing_metadata_is_fine(self):
        self.assertEqual(check.offenders(self.root), [])


if __name__ == "__main__":
    unittest.main()

"""Tests for next-version.py. Run: python3 -m unittest discover -s scripts -p 'test_*.py'"""

import importlib.util
import os
import unittest
from unittest import mock

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("next_version", os.path.join(HERE, "next-version.py"))
next_version = importlib.util.module_from_spec(spec)
spec.loader.exec_module(next_version)


class BumpForTest(unittest.TestCase):
    def test_nothing_user_facing_releases_nothing(self):
        self.assertIsNone(next_version.bump_for([]))

    def test_fix_and_perf_bump_patch(self):
        self.assertEqual(next_version.bump_for(["fixed"]), "patch")
        self.assertEqual(next_version.bump_for(["faster"]), "patch")

    def test_feat_wins_over_fix(self):
        self.assertEqual(next_version.bump_for(["fixed", "new", "faster"]), "minor")

    def test_breaking_wins_over_everything(self):
        self.assertEqual(next_version.bump_for(["new", "breaking", "fixed"]), "major")


class BumpedTest(unittest.TestCase):
    def test_patch(self):
        self.assertEqual(next_version.bumped("1.8.4", "patch"), "1.8.5")

    def test_minor_resets_patch(self):
        self.assertEqual(next_version.bumped("1.8.4", "minor"), "1.9.0")

    def test_major_resets_minor_and_patch(self):
        self.assertEqual(next_version.bumped("1.8.4", "major"), "2.0.0")


class ForcedBumpTest(unittest.TestCase):
    """--bump, as the phone's "Run workflow" passes it."""

    def setUp(self):
        for name, fake in [("release_tags", lambda: ["v1.8.4"]),
                           ("sections", lambda _range: [])]:  # only chore/docs commits
            patcher = mock.patch.object(next_version.changelog, name, fake)
            patcher.start()
            self.addCleanup(patcher.stop)

    def commits_since_tag(self, count):
        patcher = mock.patch.object(next_version.changelog, "git", lambda *args: str(count))
        patcher.start()
        self.addCleanup(patcher.stop)

    def test_forced_bump_releases_without_user_facing_commits(self):
        self.commits_since_tag(3)
        self.assertIsNone(next_version.next_version()["next"])
        self.assertEqual(next_version.next_version("patch")["next"], "1.8.5")

    def test_tagged_head_releases_nothing_even_forced(self):
        self.commits_since_tag(0)
        self.assertIsNone(next_version.next_version("minor")["next"])


class ClassifyTest(unittest.TestCase):
    """The commit reading shared with changelog.py, as the bump sees it."""

    def kind(self, subject, body=""):
        found = next_version.changelog.classify(subject, body)
        return found[0] if found else None

    def test_types(self):
        self.assertEqual(self.kind("feat: pods list"), "new")
        self.assertEqual(self.kind("fix(ios): restart button"), "fixed")
        self.assertIsNone(self.kind("chore: badge"))
        self.assertIsNone(self.kind("Merge pull request #1 from x/y"))

    def test_breaking(self):
        self.assertEqual(self.kind("feat!: drop Android 9"), "breaking")
        self.assertEqual(self.kind("refactor: config", "BREAKING CHANGE: new format"), "breaking")


if __name__ == "__main__":
    unittest.main()

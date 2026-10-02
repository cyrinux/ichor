"""Tests for play-notes.py. Run: python3 -m unittest discover -s scripts -p 'test_*.py'"""

import importlib.util
import os
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("play_notes", os.path.join(HERE, "play-notes.py"))
play_notes = importlib.util.module_from_spec(spec)
spec.loader.exec_module(play_notes)


def release(*sections, build=42):
    return {
        "version": "1.2.3",
        "build_number": build,
        "sections": [{"kind": k, "title": k.title(), "items": items} for k, items in sections],
    }


class RenderTest(unittest.TestCase):
    def test_one_bullet_per_item_in_section_order(self):
        text = play_notes.render(release(("new", ["Backups", "Insights"]), ("fixed", ["Layout"])))
        self.assertEqual(text, "• Backups\n• Insights\n• Layout\n")

    def test_release_without_user_facing_changes_gets_fallback(self):
        self.assertEqual(play_notes.render(release()), play_notes.FALLBACK + "\n")

    def test_capped_on_a_line_boundary(self):
        items = [f"Item number {i} " + "x" * 40 for i in range(20)]
        text = play_notes.render(release(("new", items)))
        self.assertLessEqual(len(text), play_notes.LIMIT)
        self.assertTrue(all(line.startswith("• Item number") for line in text.splitlines()))

    def test_single_overlong_line_is_cut_with_ellipsis(self):
        text = play_notes.render(release(("new", ["y" * 900])))
        self.assertLessEqual(len(text), play_notes.LIMIT)
        self.assertTrue(text.rstrip("\n").endswith("…"))


class WriteTest(unittest.TestCase):
    def test_keeps_a_hand_written_file(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = os.path.join(tmp, "42.txt")
            with open(path, "w", encoding="utf-8") as f:
                f.write("Curated\n")
            self.assertFalse(play_notes.write(path, "• Generated\n", force=False))
            with open(path, encoding="utf-8") as f:
                self.assertEqual(f.read(), "Curated\n")

    def test_force_overwrites_and_creates_directories(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = os.path.join(tmp, "changelogs", "42.txt")
            self.assertTrue(play_notes.write(path, "• Generated\n", force=False))
            self.assertTrue(play_notes.write(path, "• Again\n", force=True))
            with open(path, encoding="utf-8") as f:
                self.assertEqual(f.read(), "• Again\n")


if __name__ == "__main__":
    unittest.main()

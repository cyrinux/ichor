"""Tests for discord-notes.py. Run: python3 -m unittest discover -s scripts -p 'test_*.py'"""

import importlib.util
import os
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("discord_notes", os.path.join(HERE, "discord-notes.py"))
discord_notes = importlib.util.module_from_spec(spec)
spec.loader.exec_module(discord_notes)


def release(*sections, published_at="2026-10-01T18:25:00+00:00"):
    return {
        "version": "1.2.3",
        "build_number": 42,
        "published_at": published_at,
        "sections": [{"kind": k, "title": k.title(), "items": items} for k, items in sections],
    }


class DescribeTest(unittest.TestCase):
    def test_headings_and_bullets_in_section_order(self):
        text = discord_notes.describe(release(("new", ["Backups", "Insights"]), ("fixed", ["Layout"])))
        self.assertEqual(text, "**New**\n• Backups\n• Insights\n\n**Fixed**\n• Layout")

    def test_release_without_user_facing_changes_gets_fallback(self):
        self.assertEqual(discord_notes.describe(release()), discord_notes.FALLBACK)

    def test_empty_section_is_skipped(self):
        text = discord_notes.describe(release(("new", []), ("fixed", ["Layout"])))
        self.assertEqual(text, "**Fixed**\n• Layout")

    def test_capped_on_a_line_boundary_with_ellipsis(self):
        items = [f"Item number {i} " + "x" * 80 for i in range(100)]
        text = discord_notes.describe(release(("new", items)))
        self.assertLessEqual(len(text), discord_notes.LIMIT)
        lines = text.splitlines()
        self.assertEqual(lines[-1], "…")
        self.assertTrue(all(line.startswith("• Item number") for line in lines[1:-1]))


class PayloadTest(unittest.TestCase):
    def test_embed_with_title_link_and_timestamp(self):
        body = discord_notes.payload(release(("new", ["Backups"])), "https://example.com/r")
        embed = body["embeds"][0]
        self.assertEqual(embed["title"], "Ichor 1.2.3")
        self.assertEqual(embed["url"], "https://example.com/r")
        self.assertEqual(embed["timestamp"], "2026-10-01T18:25:00+00:00")
        self.assertEqual(embed["description"], "**New**\n• Backups")

    def test_no_link_without_url(self):
        self.assertNotIn("url", discord_notes.payload(release())["embeds"][0])

    def test_mentions_are_disabled(self):
        body = discord_notes.payload(release(("new", ["Ping @everyone"])))
        self.assertEqual(body["allowed_mentions"], {"parse": []})


if __name__ == "__main__":
    unittest.main()

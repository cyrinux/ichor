"""Tests for site-i18n.py. Run: python3 -m unittest discover -s scripts -p 'test_*.py'"""

import importlib.util
import os
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("site_i18n", os.path.join(HERE, "site-i18n.py"))
site_i18n = importlib.util.module_from_spec(spec)
spec.loader.exec_module(site_i18n)


def strings(html):
    return [site_i18n.normalize(span[3]) for span in site_i18n.extract(html)]


class ExtractTest(unittest.TestCase):
    def test_leaf_blocks_with_their_inline_markup(self):
        html = "<div><h2>Title</h2><p>Use <code>talosctl</code>\n   now.</p></div>"
        self.assertEqual(strings(html), ["Title", "Use <code>talosctl</code> now."])

    def test_block_holding_blocks_is_not_a_string(self):
        html = "<ul><li><h3>Head</h3><p>Body</p></li></ul>"
        self.assertEqual(strings(html), ["Head", "Body"])

    def test_translate_no_excludes_element_and_descendants(self):
        html = '<table><tr><td translate="no">talosctl get</td><td>Overview</td></tr></table>'
        self.assertEqual(strings(html), ["Overview"])

    def test_explicit_marker_and_attributes(self):
        html = ('<nav aria-label="Primary"><a data-i18n href="#x">Install</a>'
                '<img src="a.png" alt="A phone"><b data-copied="Copied" data-i18n-attr="data-copied">'
                '</b></nav>')
        self.assertEqual(strings(html), ["Primary", "Install", "A phone", "Copied"])

    def test_strings_without_letters_are_skipped(self):
        self.assertEqual(strings("<p>3:12</p><p>7/8</p>"), [])


class RenderTest(unittest.TestCase):
    def test_translates_and_reports_missing(self):
        html = "<h1>Hello</h1><p>World</p>"
        missing = set()
        out = site_i18n.render(html, site_i18n.extract(html), {"Hello": "Bonjour"}, missing)
        self.assertEqual(out, "<h1>Bonjour</h1><p>World</p>")
        self.assertEqual(missing, {"World"})


class LocalizeUrlsTest(unittest.TestCase):
    def test_assets_move_up_pages_stay_local(self):
        html = ('<html lang="en"><img src="brand/ichor.svg"><a href="privacy.html">P</a>'
                '<a href="#faq">F</a><a href="https://x.dev">X</a><a href="./">Home</a>')
        out = site_i18n.localize_urls(html, "fr", "index.html")
        self.assertIn('<html lang="fr"', out)
        self.assertIn('src="../brand/ichor.svg"', out)
        for kept in ('href="privacy.html"', 'href="#faq"', 'href="https://x.dev"', 'href="./"'):
            self.assertIn(kept, out)

    def test_language_links_and_current_language(self):
        html = ('<a href="./" hreflang="en" aria-current="true">English</a>'
                '<a href="fr/privacy.html" hreflang="fr">Français</a>'
                '<a href="de/privacy.html" hreflang="de">Deutsch</a>')
        out = site_i18n.localize_urls(html, "fr", "privacy.html")
        self.assertIn('<a href="../privacy.html" hreflang="en">', out)
        self.assertIn('<a aria-current="true" href="../fr/privacy.html" hreflang="fr">', out)
        self.assertIn('<a href="../de/privacy.html" hreflang="de">', out)

    def test_canonical_points_at_the_translation(self):
        html = ('<link rel="canonical" href="https://cyrinux.github.io/ichor/" />'
                '<meta property="og:url" content="https://cyrinux.github.io/ichor/" />')
        out = site_i18n.localize_urls(html, "uk", "index.html")
        self.assertEqual(out.count("https://cyrinux.github.io/ichor/uk/"), 2)


if __name__ == "__main__":
    unittest.main()

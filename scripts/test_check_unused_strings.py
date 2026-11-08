"""Tests for check-unused-strings.py. Run: python3 -m unittest discover -s scripts -p 'test_*.py'"""

import importlib.util
import os
import tempfile
import unittest
from pathlib import Path

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("check_unused_strings", os.path.join(HERE, "check-unused-strings.py"))
check = importlib.util.module_from_spec(spec)
spec.loader.exec_module(check)

STRINGS = """<resources>
    <string name="nodes_title">Nodes</string>
    <string name="zz_unused">Never shown</string>
    <string name="app_label" translatable="false">Ichor</string>
    <plurals name="pods_count">
        <item quantity="one">%d pod</item>
        <item quantity="other">%d pods</item>
    </plurals>
    <string-array name="themes"><item>Light</item></string-array>
</resources>
"""


class UnusedStringsTest(unittest.TestCase):
    def test_defined_keys(self):
        self.assertEqual(check.defined_keys(STRINGS), ["nodes_title", "zz_unused", "app_label", "pods_count", "themes"])

    def test_references(self):
        kotlin = 'Text(stringResource(R.string.nodes_title))\npluralStringResource(R.plurals.pods_count, n, n)\nname.levis.ichor.R.array.themes'
        manifest = '<application android:label="@string/app_label">'
        self.assertEqual(check.referenced_names(kotlin), {"nodes_title", "pods_count", "themes"})
        self.assertEqual(check.referenced_names(manifest), {"app_label"})

    def test_unused_keys(self):
        referenced = check.referenced_names("R.string.nodes_title R.plurals.pods_count R.array.themes @string/app_label")
        self.assertEqual(check.unused_keys(check.defined_keys(STRINGS), referenced, set()), ["zz_unused"])
        self.assertEqual(check.unused_keys(check.defined_keys(STRINGS), referenced, {"zz_unused"}), [])

    def test_source_files_skip_build_outputs(self):
        with tempfile.TemporaryDirectory() as tmp:
            module = Path(tmp) / "app"
            (module / "src/main/java").mkdir(parents=True)
            (module / "build/generated").mkdir(parents=True)
            (module / "src/main/java/Screen.kt").write_text("R.string.nodes_title")
            (module / "build/generated/R.java").write_text("R.string.zz_unused")
            (module / "src/main/java/notes.txt").write_text("R.string.zz_unused")

            found = [p.name for p in check.source_files([module])]
            self.assertEqual(found, ["Screen.kt"])


if __name__ == "__main__":
    unittest.main()

"""Tests for check-ios-strings.py. Run: python3 -m unittest discover -s scripts -p 'test_*.py'"""

import contextlib
import importlib.util
import io
import os
import tempfile
import unittest
from pathlib import Path

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("check_ios_strings", os.path.join(HERE, "check-ios-strings.py"))
check = importlib.util.module_from_spec(spec)
spec.loader.exec_module(check)


class UnformattedKeysTest(unittest.TestCase):
    def test_helper_that_ignores_its_argument(self):
        src = """enum Style {
    static func desired(_ a: String) -> String { String(localized: "wants %@ replicas") }
    static func managedBy(_ a: String, _ b: String) -> String { String(localized: "managed by %1$@ (%2$@)") }
    static func count(_ n: Int) -> String { String(localized: "%lld nodes") }
}
"""
        self.assertEqual(
            check.unformatted_keys(src),
            [(2, "wants %@ replicas"), (3, "managed by %1$@ (%2$@)"), (4, "%lld nodes")],
        )

    def test_format_forms_are_clean(self):
        src = """let a = String(format: String(localized: "wants %@ replicas"), x)
let b = String(
    format: String(localized: "managed by %1$@ (%2$@)"),
    owner, kind
)
let c = String(format: up ? String(localized: "up %@") : String(localized: "down %@"), x)
let d = String.localizedStringWithFormat(String(localized: "%lld nodes"), n)
let e = NSString(format: String(localized: "%@ pods") as NSString, n)
Text(verbatim: String(format: String(localized: "I/O wait: %@%% · VM steal: %@%%"), w, s))
"""
        self.assertEqual(check.unformatted_keys(src), [])

    def test_keys_without_a_specifier_are_clean(self):
        src = """let a = String(localized: "Reboot")
let b = String(localized: "Rebuilding \\(volume.progress)%")
let c = String(localized: "\\(Int(f.value))% of all requests, mostly \\(f.verb)")
let d = String(localized: "100%% done")
"""
        self.assertEqual(check.unformatted_keys(src), [])

    def test_text_is_not_this_checks_business(self):
        src = 'Text("%@ replicas")\nText(verbatim: "%@")\n'
        self.assertEqual(check.unformatted_keys(src), [])

    def test_comments_and_previews_are_ignored(self):
        src = """// String(localized: "wants %@ replicas")
/* String(localized: "last scaled %@") */
#Preview {
    Text(String(localized: "Open %@"))
}
let x = String(localized: "%@ claims")
"""
        self.assertEqual(check.unformatted_keys(src), [(6, "%@ claims")])

    def test_format_outside_the_statement_does_not_count(self):
        src = """let a = String(format: "%@", b)
let c = String(localized: "last scaled %@")
"""
        self.assertEqual(check.unformatted_keys(src), [(2, "last scaled %@")])


    def test_a_later_argument_of_format_is_not_the_format(self):
        src = """let a = String(format: "%@: %@", a, String(localized: "b %@"))
let c = String(format: fmt,
    String(localized: "x %@"))
"""
        self.assertEqual(check.unformatted_keys(src), [(1, "b %@"), (3, "x %@")])

    def test_key_inside_an_interpolation(self):
        src = 'Text("\\(String(localized: "%lld pods")) ready")\n'
        self.assertEqual(check.unformatted_keys(src), [(1, "%lld pods")])

    def test_raw_strings_and_nested_comments_keep_the_lexer_in_step(self):
        src = """let r = #"say "hi" \\(x"#
/* a /* b */ "c */
let s = String(localized: "x %@")
let t = String(localized: #"raw %@"#)
"""
        self.assertEqual(check.unformatted_keys(src), [(3, "x %@"), (4, "raw %@")])


class MainTest(unittest.TestCase):
    def test_exit_code_and_report(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "Bad.swift").write_text('let a = String(localized: "Open %@")\n')
            (root / "Good.swift").write_text('let a = String(format: String(localized: "Open %@"), x)\n')
            self.assertEqual(check.findings([root]), [(root / "Bad.swift", 1, "Open %@")])
            self.assertEqual(check.main([str(root / "Good.swift")]), 0)
            out = io.StringIO()
            with contextlib.redirect_stdout(out), contextlib.redirect_stderr(io.StringIO()):
                self.assertEqual(check.main([str(root)]), 1)
            self.assertIn("Bad.swift:1: Open %@", out.getvalue())


if __name__ == "__main__":
    unittest.main()

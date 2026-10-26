"""Tests for go-licenses.py. Run: python3 -m unittest discover -s scripts -p 'test_*.py'"""

import importlib.util
import json
import os
import tempfile
import unittest
import unittest.mock
from pathlib import Path

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("go_licenses", os.path.join(HERE, "go-licenses.py"))
go_licenses = importlib.util.module_from_spec(spec)
spec.loader.exec_module(go_licenses)

MIT = "Permission is hereby granted, free of charge, to any person obtaining a copy"
MPL = "Mozilla Public License Version 2.0\n... GNU General Public License ..."


class IdentifyTest(unittest.TestCase):
    def test_mpl_mentioning_the_gpl_is_mpl(self):
        self.assertEqual(go_licenses.identify(MPL), "MPL-2.0")

    def test_whitespace_and_case_are_ignored(self):
        self.assertEqual(go_licenses.identify("PERMISSION is hereby\n  granted, free of charge"), "MIT")

    def test_unknown_text(self):
        self.assertIsNone(go_licenses.identify("All rights reserved."))


class LibraryTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.dir = Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_entry_keeps_the_full_text(self):
        (self.dir / "LICENSE").write_text(MIT + "\n")
        (self.dir / "README.md").write_text("not a license")
        entry = go_licenses.library("pkg", "v1.0.0", "https://example.com", self.dir)
        self.assertEqual(entry, {"name": "pkg", "version": "v1.0.0", "website": "https://example.com",
                                 "licenses": ["MIT"], "text": MIT})

    def test_no_recognised_license(self):
        (self.dir / "LICENSE").write_text("All rights reserved.")
        self.assertIsNone(go_licenses.library("pkg", "v1", "", self.dir))


class SwiftTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        self.checkouts = self.root / "checkouts"
        (self.checkouts / "SwiftTerm").mkdir(parents=True)
        (self.checkouts / "SwiftTerm" / "LICENSE").write_text(MIT)
        self.resolved = self.root / "Package.resolved"
        self.resolved.write_text(json.dumps({"version": 3, "pins": [{
            "identity": "swiftterm", "location": "https://github.com/migueldeicaza/SwiftTerm.git",
            "state": {"version": "1.20.0", "revision": "abc"}}]}))

    def tearDown(self):
        self.tmp.cleanup()

    def test_version_and_website_from_package_resolved(self):
        unknown = []
        libraries = go_licenses.swift_libraries(self.checkouts, self.resolved, unknown)
        self.assertEqual(unknown, [])
        self.assertEqual([(l["name"], l["version"], l["website"], l["licenses"]) for l in libraries],
                         [("SwiftTerm", "1.20.0", "https://github.com/migueldeicaza/SwiftTerm", ["MIT"])])

    def test_v1_package_resolved(self):
        self.resolved.write_text(json.dumps({"object": {"pins": [{
            "package": "SwiftTerm", "repositoryURL": "https://github.com/migueldeicaza/SwiftTerm",
            "state": {"version": None, "revision": "0123456789abcdef"}}]}, "version": 1}))
        self.assertEqual(go_licenses.swift_pins(self.resolved)["swiftterm"]["version"], "0123456789ab")

    def test_unrecognised_checkout_is_reported(self):
        (self.checkouts / "Mystery").mkdir()
        unknown = []
        go_licenses.swift_libraries(self.checkouts, None, unknown)
        self.assertEqual(unknown, ["Mystery"])


if __name__ == "__main__":
    unittest.main()


class LlamaTest(unittest.TestCase):
    def test_android_entry_names_the_pinned_release(self):
        root = Path(HERE).parent
        pin = dict(line.split("=", 1) for line in (root / "native/llama-cpp.env").read_text().splitlines()
                   if "=" in line and not line.startswith("#"))
        entry = json.loads((root / "app/aboutlibraries/libraries/llama-cpp.json").read_text())
        self.assertEqual(entry["artifactVersion"], pin["LLAMA_TAG"])

    def test_missing_source_is_reported(self):
        unknown = []
        with unittest.mock.patch.object(go_licenses, "ROOT", Path(tempfile.mkdtemp())) as root:
            (root / "native").mkdir()
            (root / "native/llama-cpp.env").write_text("LLAMA_TAG=b1\nLLAMA_COMMIT=abc\n")
            self.assertEqual(go_licenses.llama_library(unknown), [])
        self.assertTrue(unknown and unknown[0].startswith("llama.cpp"))

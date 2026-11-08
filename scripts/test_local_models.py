"""Tests for local-models.py. Run: python3 -m unittest discover -s scripts -p 'test_*.py'"""
import importlib.util
import json
import os
import tempfile
import unittest
from pathlib import Path

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("local_models", os.path.join(HERE, "local-models.py"))
local_models = importlib.util.module_from_spec(spec)
spec.loader.exec_module(local_models)

REV = "a" * 40
SHA = "b" * 64


def fake_api(url):
    if url.endswith("/revision/main"):
        return {"sha": REV}
    if url.endswith("/tree/" + REV):
        return [{"path": "README.md"}, {"path": "m-Q4_K_M.gguf", "lfs": {"oid": SHA, "size": 2_800_000_000}}]
    raise AssertionError(url)


class PinTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.catalog = Path(self.tmp.name) / "local_models.json"
        self.catalog.write_text(json.dumps([
            {"id": "m", "repo": "org/m-GGUF", "file": "m-Q4_K_M.gguf", "revision": "", "bytes": 0, "sha256": "",
             "assistantPrefix": "<think>\n\n</think>\n\n"},
        ], indent=2) + "\n")

    def tearDown(self):
        self.tmp.cleanup()

    def test_pins_revision_size_and_digest(self):
        self.assertEqual(local_models.main([], fetch=fake_api, catalog=self.catalog), 0)
        entry = json.loads(self.catalog.read_text())[0]
        self.assertEqual((entry["revision"], entry["bytes"], entry["sha256"]), (REV, 2_800_000_000, SHA))
        self.assertEqual(entry["assistantPrefix"], "<think>\n\n</think>\n\n")
        self.assertEqual(local_models.main(["--check"], catalog=self.catalog), 0)

    def test_check_fails_unpinned(self):
        self.assertEqual(local_models.main(["--check"], catalog=self.catalog), 1)

    def test_unknown_file_fails(self):
        entry = {"id": "x", "repo": "org/m-GGUF", "file": "other.gguf"}
        with self.assertRaises(SystemExit):
            local_models.pin(entry, fetch=fake_api)

    def test_the_app_catalog_parses(self):
        entries = json.loads(local_models.CATALOG.read_text())
        self.assertTrue(all({"id", "repo", "file"} <= e.keys() for e in entries))


if __name__ == "__main__":
    unittest.main()

"""Tests for qr-parts.py. Run: python3 -m unittest discover -s scripts -p 'test_*.py'"""

import base64
import gzip
import importlib.util
import os
import re
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("qr_parts", os.path.join(HERE, "qr-parts.py"))
qr_parts = importlib.util.module_from_spec(spec)
spec.loader.exec_module(qr_parts)

# Shared with go/ichorgo/qr_parts_test.go (TestQRPartsJoinScriptOutput): an invented
# talosconfig and this script's parts for it (--chunk 800), which the Go side joins.
FIXTURE = os.path.join(HERE, "..", "go", "ichorgo", "testdata", "qr_parts")

# What the app's QRPart accepts.
PART = re.compile(r"^ichor:(\d+)/(\d+):([A-Za-z0-9_-]{1,2048})$")


def read(name, mode="r"):
    with open(os.path.join(FIXTURE, name), mode) as f:
        return f.read()


class PartsTest(unittest.TestCase):
    def test_round_trip(self):
        data = os.urandom(5000)
        texts = qr_parts.parts(data)
        self.assertEqual(len(texts), 6)
        self.assertEqual(qr_parts.join(reversed(texts)), data)

    def test_parts_match_the_app_format(self):
        texts = qr_parts.parts(os.urandom(3000))
        for i, text in enumerate(texts, 1):
            m = PART.match(text)
            self.assertIsNotNone(m, text[:20])
            self.assertEqual((int(m[1]), int(m[2])), (i, len(texts)))
            self.assertLessEqual(len(m[3]), qr_parts.CHUNK)

    def test_chunks_are_unpadded_base64url_of_gzip(self):
        texts = qr_parts.parts(b"context: a\n", chunk=1200)
        self.assertEqual(len(texts), 1)
        chunk = texts[0].split(":", 2)[2]
        self.assertNotIn("=", chunk)
        raw = base64.urlsafe_b64decode(chunk + "=" * (-len(chunk) % 4))
        self.assertEqual(raw[:2], b"\x1f\x8b")
        self.assertEqual(gzip.decompress(raw), b"context: a\n")

    def test_same_config_same_codes(self):
        data = b"context: a\n" * 50
        self.assertEqual(qr_parts.parts(data), qr_parts.parts(data))

    def test_too_many_parts(self):
        with self.assertRaises(ValueError):
            qr_parts.parts(os.urandom(20000), chunk=100)

    def test_chunk_size_limits(self):
        for chunk in (0, qr_parts.CHUNK_MAX + 1):
            with self.assertRaises(ValueError):
                qr_parts.parts(b"x", chunk=chunk)

    def test_join_refuses_incomplete_or_mixed_sets(self):
        texts = qr_parts.parts(os.urandom(3000))
        with self.assertRaises(ValueError):
            qr_parts.join(texts[1:])
        with self.assertRaises(ValueError):
            qr_parts.join([texts[0], "ichor:2/9:QUJD"])


class FixtureTest(unittest.TestCase):
    """The checked-in parts the Go tests join come from this script."""

    def test_fixture_parts_decode_to_the_config(self):
        texts = read("parts.txt").split()
        self.assertGreaterEqual(len(texts), 3)
        for text in texts:
            self.assertIsNotNone(PART.match(text), text[:20])
        self.assertEqual(qr_parts.join(reversed(texts)), read("talosconfig.yaml", "rb"))

    def test_fixture_matches_the_script(self):
        # Same headers; the chunks themselves may differ by a byte between zlib builds.
        config = read("talosconfig.yaml", "rb")
        texts = qr_parts.parts(config, chunk=800)
        heads = [t.split(":", 2)[1] for t in texts]
        self.assertEqual(heads, [t.split(":", 2)[1] for t in read("parts.txt").split()])
        self.assertEqual(qr_parts.join(texts), config)


if __name__ == "__main__":
    unittest.main()

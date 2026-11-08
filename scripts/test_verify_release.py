"""Tests for verify-release.py. Run: python3 -m unittest discover -s scripts -p 'test_*.py'"""

import importlib.util
import io
import os
import struct
import tempfile
import unittest
import zipfile

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("verify_release", os.path.join(HERE, "verify-release.py"))
verify_release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(verify_release)

DATE = (2008, 1, 1, 0, 0, 0)


def make_zip(files: dict, signed: bool = False) -> bytes:
    """A zip of files (name -> bytes) in order; signed adds v1 signature files."""
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w") as z:
        items = dict(files)
        if signed:
            items.update({"META-INF/MANIFEST.MF": b"Manifest-Version: 1.0\n", "META-INF/CERT.SF": b"sf", "META-INF/CERT.RSA": b"rsa"})
        for name, data in items.items():
            info = zipfile.ZipInfo(name, DATE)
            info.compress_type = zipfile.ZIP_STORED if name.endswith(".so") else zipfile.ZIP_DEFLATED
            z.writestr(info, data)
    return buf.getvalue()


def with_signing_block(data: bytes, payload: bytes = b"\x01" * 40) -> bytes:
    """data with an APK Signing Block inserted before its central directory, the offset fixed."""
    eocd = data.rfind(verify_release.EOCD_MAGIC)
    cd = struct.unpack_from("<I", data, eocd + 16)[0]
    pairs = struct.pack("<Q", len(payload) + 4) + struct.pack("<I", 0x7109871A) + payload
    size = len(pairs) + 8 + 16
    block = struct.pack("<Q", size) + pairs + struct.pack("<Q", size) + verify_release.APK_SIG_MAGIC
    new_cd = cd + len(block)
    eocd_fixed = data[eocd:eocd + 16] + struct.pack("<I", new_cd) + data[eocd + 20:]
    return data[:cd] + block + data[cd:eocd] + eocd_fixed


APP = {"AndroidManifest.xml": b"<manifest/>", "classes.dex": b"dex\n035" + b"\x00" * 64, "lib/arm64-v8a/libgojni.so": b"\x7fELF" * 32}


class CompareTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.TemporaryDirectory()
        self.addCleanup(self.dir.cleanup)

    def write(self, name: str, data: bytes) -> str:
        path = os.path.join(self.dir.name, name)
        with open(path, "wb") as f:
            f.write(data)
        return path

    def test_equal_once_both_signatures_are_stripped(self):
        published = self.write("published.apk", with_signing_block(make_zip(APP, signed=True)))
        rebuilt = self.write("rebuilt.apk", make_zip(APP))
        self.assertEqual(verify_release.compare(published, rebuilt, None), 0)

    def test_a_changed_classes_dex_is_named(self):
        changed = dict(APP, **{"classes.dex": b"dex\n035" + b"\x01" * 64})
        published = self.write("published.apk", with_signing_block(make_zip(APP)))
        rebuilt = self.write("rebuilt.apk", make_zip(changed))
        report = os.path.join(self.dir.name, "diff.txt")
        self.assertEqual(verify_release.compare(published, rebuilt, report), 1)
        with open(report) as f:
            lines = f.read().splitlines()
        self.assertEqual(len(lines), 1)
        self.assertTrue(lines[0].startswith("differs: classes.dex"), lines)

    def test_an_extra_entry_is_named(self):
        published = self.write("published.apk", make_zip(APP))
        rebuilt = self.write("rebuilt.apk", make_zip(dict(APP, **{"assets/build-time.txt": b"now"})))
        diff = verify_release.differences(verify_release.entries(published), verify_release.entries(rebuilt))
        self.assertEqual(diff, ["only rebuilt: assets/build-time.txt size=3 crc=" + format(zipfile.crc32(b"now"), "08x")])

    def test_the_order_counts(self):
        reordered = dict(reversed(list(APP.items())))
        diff = verify_release.differences(
            verify_release.entries(self.write("a.apk", make_zip(APP))),
            verify_release.entries(self.write("b.apk", make_zip(reordered))),
        )
        self.assertEqual(diff, ["same entries in another order"])

    def test_keep_writes_stripped_apks_that_compare_equal(self):
        published = self.write("published.apk", with_signing_block(make_zip(APP, signed=True)))
        stripped = os.path.join(self.dir.name, "published-stripped.apk")
        verify_release.write_stripped(published, stripped)
        with zipfile.ZipFile(stripped) as z:
            self.assertEqual([i.filename for i in z.infolist()], list(APP))


class SigningBlockTest(unittest.TestCase):
    def test_removing_the_block_gives_back_the_unsigned_bytes(self):
        plain = make_zip(APP)
        signed = with_signing_block(plain)
        self.assertIn(verify_release.APK_SIG_MAGIC, signed)
        self.assertEqual(verify_release.strip_signing_block(signed), plain)
        with zipfile.ZipFile(io.BytesIO(verify_release.strip_signing_block(signed))) as z:
            self.assertIsNone(z.testzip())

    def test_a_zip_without_a_block_is_unchanged(self):
        plain = make_zip(APP)
        self.assertEqual(verify_release.strip_signing_block(plain), plain)

    def test_a_malformed_block_is_an_error(self):
        signed = bytearray(with_signing_block(make_zip(APP)))
        start = signed.find(struct.pack("<I", 0x7109871A)) - 16
        signed[start:start + 8] = struct.pack("<Q", 3)  # the leading size no longer matches
        with self.assertRaises(verify_release.VerifyError):
            verify_release.strip_signing_block(bytes(signed))

    def test_not_a_zip(self):
        with self.assertRaises(verify_release.VerifyError):
            verify_release.strip_signing_block(b"not a zip")


class CommandLineTest(unittest.TestCase):
    def test_compare_exit_codes(self):
        with tempfile.TemporaryDirectory() as d:
            a = os.path.join(d, "a.apk")
            with open(a, "wb") as f:
                f.write(make_zip(APP))
            self.assertEqual(verify_release.main(["compare", a, a]), 0)
            self.assertEqual(verify_release.main(["compare", a, os.path.join(d, "missing.apk")]), 2)

    def test_an_unknown_abi_is_refused(self):
        with self.assertRaises(SystemExit):
            verify_release.main(["verify", "v1.0.0", "--abi", "mips"])


class PublishedOptionTest(unittest.TestCase):
    """--published: the APK is already on disk (a draft downloaded by an earlier CI step)."""

    def setUp(self):
        self.dir = tempfile.TemporaryDirectory()
        self.addCleanup(self.dir.cleanup)
        self.calls = []
        self.originals = (verify_release.download, verify_release.rebuild)
        self.addCleanup(self.restore)

        def fake_download(*args):
            self.calls.append("download")
            raise AssertionError("downloaded although --published was given")

        def fake_rebuild(tag, abi, repo, work):
            self.calls.append("rebuild")
            path = os.path.join(work, "rebuilt.apk")
            with open(path, "wb") as f:
                f.write(make_zip(APP))
            return path

        verify_release.download, verify_release.rebuild = fake_download, fake_rebuild

    def restore(self):
        verify_release.download, verify_release.rebuild = self.originals

    def test_the_given_apk_is_compared_without_a_download(self):
        published = os.path.join(self.dir.name, "ichor-v1.0.0-arm64-v8a.apk")
        with open(published, "wb") as f:
            f.write(with_signing_block(make_zip(APP, signed=True)))
        self.assertEqual(verify_release.main(["verify", "v1.0.0", "--published", published]), 0)
        self.assertEqual(self.calls, ["rebuild"])
        self.assertTrue(os.path.exists(published), "the caller's APK must not be deleted")

    def test_a_missing_apk_fails_before_the_build(self):
        missing = os.path.join(self.dir.name, "missing.apk")
        self.assertEqual(verify_release.main(["verify", "v1.0.0", "--published", missing]), 2)
        self.assertEqual(self.calls, [])


if __name__ == "__main__":
    unittest.main()

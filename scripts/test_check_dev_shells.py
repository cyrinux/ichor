"""Tests for check-dev-shells.py. Run: python3 -m unittest discover -s scripts -p 'test_*.py'"""

import importlib.util
import os
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("check_dev_shells", os.path.join(HERE, "check-dev-shells.py"))
check = importlib.util.module_from_spec(spec)
spec.loader.exec_module(check)


def drv(system: str) -> dict:
    return {"system": system, "env": {}}


class ForeignTest(unittest.TestCase):
    def test_aarch64_takes_its_own_and_emulated_x86_64(self):
        doc = {"derivations": {"a-go.drv": drv("aarch64-linux"), "b-ndk.drv": drv("x86_64-linux"), "c-fetch.drv": drv("builtin")}}
        self.assertEqual(check.foreign(doc, "aarch64-linux"), [])

    def test_aarch64_refuses_i686(self):
        doc = {"derivations": {"a-go.drv": drv("aarch64-linux"), "x-ncurses-abi5-compat-6.6.drv": drv("i686-linux")}}
        self.assertEqual(check.foreign(doc, "aarch64-linux"), [("i686-linux", "x-ncurses-abi5-compat-6.6.drv")])

    def test_x86_64_builds_i686_natively(self):
        doc = {"derivations": {"x-ncurses.drv": drv("i686-linux"), "y-sdk.drv": drv("x86_64-linux")}}
        self.assertEqual(check.foreign(doc, "x86_64-linux"), [])

    def test_x86_64_refuses_aarch64(self):
        doc = {"derivations": {"z-clang.drv": drv("aarch64-linux")}}
        self.assertEqual(check.foreign(doc, "x86_64-linux"), [("aarch64-linux", "z-clang.drv")])

    def test_older_nix_keys_store_paths_at_the_top(self):
        doc = {"/nix/store/x-ncurses.drv": drv("i686-linux"), "/nix/store/a-go.drv": drv("aarch64-linux")}
        self.assertEqual(check.foreign(doc, "aarch64-linux"), [("i686-linux", "/nix/store/x-ncurses.drv")])

    def test_sorted_by_name(self):
        doc = {"derivations": {"b.drv": drv("i686-linux"), "a.drv": drv("riscv64-linux")}}
        self.assertEqual([name for _, name in check.foreign(doc, "aarch64-linux")], ["a.drv", "b.drv"])

    def test_unknown_host(self):
        with self.assertRaises(ValueError):
            check.foreign({"derivations": {}}, "riscv64-linux")


class ToBuildTest(unittest.TestCase):
    def test_only_the_built_list(self):
        report = (
            "these 2 derivations will be built:\n"
            "  /nix/store/aaa-androidsdk.drv\n"
            "  /nix/store/bbb-ncurses-abi5-compat-6.6.drv\n"
            "these 3 paths will be fetched (1.0 MiB download, 2.0 MiB unpacked):\n"
            "  /nix/store/ccc-go-1.27\n"
            "  /nix/store/ddd-source.drv\n"
        )
        self.assertEqual(check.to_build(report), ["/nix/store/aaa-androidsdk.drv", "/nix/store/bbb-ncurses-abi5-compat-6.6.drv"])

    def test_one_derivation(self):
        report = "this derivation will be built:\n  /nix/store/aaa-nix-shell.drv\n"
        self.assertEqual(check.to_build(report), ["/nix/store/aaa-nix-shell.drv"])

    def test_nothing_to_build(self):
        report = "evaluation warning: Dispatch has been renamed\nthis path will be fetched (1 MiB download):\n  /nix/store/eee-swift-6.2.4\n"
        self.assertEqual(check.to_build(report), [])


if __name__ == "__main__":
    unittest.main()

#!/usr/bin/env python3
"""Shows a config as several QR codes, for the app's multi-part QR import.

A config too large for one code, even gzipped (`just qr-gz`), is gzipped (-9), encoded as
base64url without padding and split into chunks of 1,200 characters. Each chunk becomes one
code reading `ichor:<i>/<n>:<chunk>` (i from 1, n at most 16): the app collects them in any
order and imports the config as a one-code scan would.

Chunk size: base64url is byte mode, and 1,200 characters plus the short header fit a
version 25 symbol at ECC level L (1,273 bytes), which phone cameras still read from a
screen. Parts may hold up to 2,048 characters; --chunk changes the size.

The codes contain the private key: show them where nobody can photograph your screen, and
delete any PNGs afterwards.

Usage:
  scripts/qr-parts.py talosconfig-phone            # prints each code in the terminal
  scripts/qr-parts.py talosconfig-phone -o DIR     # writes DIR/part-1-of-3.png, ...
  scripts/qr-parts.py talosconfig-phone --text     # prints the part texts only
"""

import argparse
import base64
import gzip
import os
import shutil
import subprocess
import sys

PREFIX = "ichor:"
CHUNK = 1200
CHUNK_MAX = 2048
PARTS_MAX = 16


def compress(data):
    """gzip -9 with no timestamp, so the same config always gives the same codes."""
    return gzip.compress(data, compresslevel=9, mtime=0)


def encode(data):
    return base64.urlsafe_b64encode(compress(data)).rstrip(b"=").decode("ascii")


def parts(data, chunk=CHUNK):
    """The part texts for data, in order."""
    if not 1 <= chunk <= CHUNK_MAX:
        raise ValueError(f"chunk size must be 1 to {CHUNK_MAX}")

    encoded = encode(data)
    chunks = [encoded[i : i + chunk] for i in range(0, len(encoded), chunk)]
    if len(chunks) > PARTS_MAX:
        raise ValueError(f"{len(chunks)} codes needed, the app reads at most {PARTS_MAX}")

    return [f"{PREFIX}{i}/{len(chunks)}:{c}" for i, c in enumerate(chunks, 1)]


def join(texts):
    """The config back from part texts in any order (what the app does), for the tests."""
    found = {}
    total = None
    for text in texts:
        head, sep, chunk = text.strip().partition(":")[2].partition(":")
        index, slash, count = head.partition("/")
        if not (text.startswith(PREFIX) and sep and slash):
            raise ValueError("not a part")
        if total not in (None, int(count)):
            raise ValueError("parts of different sets")
        total = int(count)
        found[int(index)] = chunk
    if total is None or sorted(found) != list(range(1, total + 1)):
        raise ValueError("missing parts")

    encoded = "".join(found[i] for i in range(1, total + 1))
    return gzip.decompress(base64.urlsafe_b64decode(encoded + "=" * (-len(encoded) % 4)))


def qrencode(args):
    if shutil.which("qrencode") is None:
        sys.exit("qrencode is not installed")
    subprocess.run(["qrencode", "-l", "L", *args], check=True)


def main():
    p = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    p.add_argument("config", help="talosconfig or kubeconfig file")
    p.add_argument("-o", "--out", metavar="DIR", help="write PNGs into DIR instead of printing")
    p.add_argument("--text", action="store_true", help="print the part texts, not codes")
    p.add_argument("--chunk", type=int, default=CHUNK, help=f"characters per code (default {CHUNK})")
    args = p.parse_args()

    with open(args.config, "rb") as f:
        try:
            texts = parts(f.read(), args.chunk)
        except ValueError as e:
            sys.exit(str(e))

    n = len(texts)
    for i, text in enumerate(texts, 1):
        if args.text:
            print(text)
        elif args.out:
            os.makedirs(args.out, exist_ok=True)
            path = os.path.join(args.out, f"part-{i}-of-{n}.png")
            qrencode(["-o", path, text])
            print(path)
        else:
            print(f"Code {i} of {n}:")
            qrencode(["-t", "ansiutf8", text])
            if i < n and sys.stdin.isatty():
                input("Press Enter for the next code...")


if __name__ == "__main__":
    main()

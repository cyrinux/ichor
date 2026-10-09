#!/usr/bin/env bash
# Runs the core package's XCTests on Linux. SwiftPM's Linux test discovery needs
# libIndexStore (missing from nixpkgs' swift), so compile with swiftc and generate the
# XCTMain entry point from the test method names. On macOS just use `swift test`.
#
#   nix develop .#swift --command ios/IchorCore/test-linux.sh
set -euo pipefail
cd "$(dirname "$0")"
out="$(mktemp -d)"
trap 'rm -rf "$out"' EXIT

main="$out/main.swift"
echo "import XCTest" >"$main"
cases=()
for file in Tests/IchorCoreTests/*.swift; do
  class="$(sed -nE 's/^final class ([A-Za-z0-9_]+): XCTestCase.*/\1/p' "$file")"
  tests="$(sed -nE 's/^ *func (test[A-Za-z0-9_]*)\(\).*/("\1", \1)/p' "$file" | paste -sd, -)"
  echo "extension $class { static var allTests = [$tests] }" >>"$main"
  cases+=("testCase($class.allTests)")
  # Compile tests in the same module, so drop the module import.
  sed '/@testable import IchorCore/d' "$file" >"$out/$(basename "$file")"
done
echo "XCTMain([$(IFS=,; echo "${cases[*]}")])" >>"$main"

# Sources in subfolders too (DataServices/...), in a stable order.
mapfile -t sources < <(find Sources/IchorCore -name "*.swift" | sort)
swiftc -o "$out/tests" "${sources[@]}" "$out"/*.swift 2>&1 | grep -v "glibc not found" || true
"$out/tests"

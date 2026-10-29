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
  # XCTest's list of (name, method) is not Sendable; written once here, read once by XCTMain.
  # The explicit type matters in Swift 6 mode: inferred, the methods are @Sendable closures,
  # which corelibs XCTest's testCase(_:) fails to cast at run time and aborts.
  echo "extension $class { nonisolated(unsafe) static var allTests: [(String, ($class) -> () throws -> Void)] = [$tests] }" >>"$main"
  cases+=("testCase($class.allTests)")
  # Compile tests in the same module, so drop the module import.
  sed '/@testable import IchorCore/d' "$file" >"$out/$(basename "$file")"
done
echo "XCTMain([$(IFS=,; echo "${cases[*]}")])" >>"$main"

# Sources in subfolders too (DataServices/...), in a stable order.
mapfile -t sources < <(find Sources/IchorCore -name "*.swift" | sort)
# Swift 6 language mode with complete concurrency checking, as Package.swift sets it for
# macOS: swiftc alone would compile in Swift 5 mode and check nothing.
built=0
swiftc -swift-version 6 -strict-concurrency=complete -o "$out/tests" "${sources[@]}" "$out"/*.swift >"$out/build.log" 2>&1 || built=$?
grep -v "glibc not found" "$out/build.log" || true
# A compile error stops here with swiftc's code, not later as a missing binary (exit 127).
if [ "$built" -ne 0 ]; then
  echo "test-linux.sh: the build failed (swiftc exit $built)." >&2
  exit "$built"
fi

# A Swift runtime trap kills the whole binary, so the suites after it never run: name the
# test it died in rather than leave a bare "Trace/breakpoint trap".
status=0
"$out/tests" 2>&1 | tee "$out/log" || status=${PIPESTATUS[0]}
if [ "$status" -ne 0 ]; then
  last="$(sed -nE "s/^Test Case '([^']+)' started.*/\1/p" "$out/log" | tail -1)"
  if [ -n "$last" ] && ! grep -qE "^Test Case '$last' (passed|failed)" "$out/log"; then
    echo "test-linux.sh: the test binary died (exit $status) in $last; later suites did not run." >&2
  fi
  exit "$status"
fi

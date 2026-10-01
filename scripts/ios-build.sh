#!/usr/bin/env bash
# Builds the iOS app on macOS (Xcode + xcodegen + Go):
#   1. Talosmobile.xcframework from the Go core (gomobile, device + simulator);
#   2. the core Swift package tests;
#   3. the Xcode project (XcodeGen) and the app.
#
#   scripts/ios-build.sh test   # core tests + simulator build (CI on every push)
#   scripts/ios-build.sh ipa    # unsigned Release build packaged as an IPA (default)
#
# The unsigned IPA can be re-signed and installed with your Apple ID by Sideloadly or
# AltStore. Set TALOS_IOS_TEAM_ID and drop CODE_SIGNING_ALLOWED=NO to sign in Xcode instead.
set -euo pipefail

MODE="${1:-ipa}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

[[ "$(uname -s)" == Darwin ]] || { echo "iOS builds need macOS with Xcode." >&2; exit 1; }
command -v xcodegen >/dev/null || { echo "xcodegen is missing (brew install xcodegen)." >&2; exit 1; }

set -a
eval "$("$ROOT/scripts/version.sh" --env)"
set +a
echo "version $TALOSDEV_MOBILE_VERSION (build $TALOSDEV_MOBILE_BUILD_NUMBER)"

# gomobile/gobind versions are pinned by the tool directive in go/go.mod.
cd "$ROOT/go"
export GOBIN="$ROOT/.cache/gobin" PATH="$ROOT/.cache/gobin:$PATH"
go install tool
rm -rf "$ROOT/ios/Frameworks/Talosmobile.xcframework"
gomobile bind -target=ios,iossimulator -iosversion=17.0 -ldflags="-s -w" \
  -o "$ROOT/ios/Frameworks/Talosmobile.xcframework" ./talosmobile

python3 "$ROOT/scripts/check-translations.py"
(cd "$ROOT/ios/TalosdevMobileCore" && swift test)

cd "$ROOT/ios"
xcodegen generate
# iOS only accepts numeric x.y.z marketing versions; the full string is in the IPA name.
common=(
  -project TalosdevMobile.xcodeproj -scheme TalosdevMobile -derivedDataPath build
  MARKETING_VERSION="$TALOSDEV_MOBILE_VERSION_BASE" CURRENT_PROJECT_VERSION="$TALOSDEV_MOBILE_BUILD_NUMBER"
  CODE_SIGNING_ALLOWED=NO
  # SwiftTerm ships a build-tool plugin; CI cannot click "Trust & Enable".
  -skipPackagePluginValidation -skipMacroValidation
)

case "$MODE" in
  test)
    xcodebuild "${common[@]}" -configuration Debug -destination 'generic/platform=iOS Simulator' build
    ;;
  ipa)
    xcodebuild "${common[@]}" -configuration Release -destination 'generic/platform=iOS' build
    rm -rf build/ipa && mkdir -p build/ipa/Payload
    cp -R build/Build/Products/Release-iphoneos/TalosdevMobile.app build/ipa/Payload/
    ipa="talosdev-mobile-v${TALOSDEV_MOBILE_VERSION//+/-}-unsigned.ipa"
    (cd build/ipa && zip -qry "../$ipa" Payload)
    echo "$ROOT/ios/build/$ipa"
    ;;
  *)
    echo "usage: $0 [test|ipa]" >&2
    exit 2
    ;;
esac

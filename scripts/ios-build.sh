#!/usr/bin/env bash
# Builds the iOS app on macOS (Xcode + xcodegen + Go + CMake):
#   1. Ichorgo.xcframework from the Go core (gomobile, device + simulator), and llama.xcframework
#      (the on-device model engine, llama.cpp);
#   2. the core Swift package tests;
#   3. the Xcode project (XcodeGen) and the app.
#
#   scripts/ios-build.sh test   # core tests + simulator build (CI on every push)
#   scripts/ios-build.sh screenshots  # the demo walk in both appearances (scripts/ios-screenshots.sh next)
#   scripts/ios-build.sh ipa    # unsigned Release build packaged as an IPA (default)
#   scripts/ios-build.sh appstore  # App Store build, signed and uploaded to App Store Connect
#   scripts/ios-build.sh watch  # the Apple Watch app and complication, unsigned, for the watchOS
#                               # simulator (not embedded in the IPA yet; no Go core needed)
#   scripts/ios-build.sh format # rewrite the Swift sources to ios/.swift-format (`just ios-format`)
#   ICHOR_SKIP_TESTS=1 scripts/ios-build.sh ipa  # no core tests (CI: the test job runs them)
#
# The unsigned IPA can be re-signed and installed with your Apple ID by Sideloadly or
# AltStore. Set ICHOR_IOS_TEAM_ID and drop CODE_SIGNING_ALLOWED=NO to sign in Xcode instead.
#
# appstore compiles with APP_STORE (no donation links, see Distribution.swift) and lets Xcode
# sign it (automatic signing, cloud-managed distribution certificate) with an App Store
# Connect API key, then uploads it (TestFlight). It needs ICHOR_IOS_TEAM_ID, ASC_KEY_PATH (the
# AuthKey_<id>.p8 file), ASC_KEY_ID and ASC_ISSUER_ID.
#
# ICHOR_IOS_MULTICAST=1 adds com.apple.developer.networking.multicast to the app's entitlements,
# for Wake-on-LAN broadcasts. Apple grants it on request only (fastlane/APP_STORE_CONNECT.md):
# signing with it before the team is approved fails, so it is off by default.
#
# ICHOR_IOS_EXPORT_CODE, once App Store Connect approved the encryption documentation, sets
# ITSEncryptionExportComplianceCode so uploads skip the export compliance questions.
set -euo pipefail

MODE="${1:-ipa}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
case "$MODE" in
  test|screenshots|ipa|watch|format) ;;
  appstore)
    : "${ICHOR_IOS_TEAM_ID:?ICHOR_IOS_TEAM_ID is not set}"
    : "${ASC_KEY_PATH:?ASC_KEY_PATH is not set (App Store Connect API key, AuthKey_<id>.p8)}"
    : "${ASC_KEY_ID:?ASC_KEY_ID is not set}"
    : "${ASC_ISSUER_ID:?ASC_ISSUER_ID is not set}"
    ;;
  *)
    echo "usage: $0 [test|screenshots|ipa|appstore|watch|format]" >&2
    exit 2
    ;;
esac

[[ "$(uname -s)" == Darwin ]] || { echo "iOS builds need macOS with Xcode." >&2; exit 1; }

# The Swift sources keep the shape of ios/.swift-format, with the swift-format of the selected
# Xcode (its rules change between releases: a runner image with a newer Xcode may ask for a
# one-time reformat). `test` lints them first, so a format slip fails in seconds, before the Go
# core and llama.cpp builds; `format` rewrites them in place. Only the source folders that exist
# are passed (no build/ or Frameworks/).
if [[ "$MODE" == test || "$MODE" == format ]]; then
  swift_dirs=()
  for dir in Ichor IchorCore/Sources IchorCore/Tests IchorShare IchorUITests IchorWatch \
    IchorWatchWidget Shared TalosWidget WatchShared; do
    [[ -d "$ROOT/ios/$dir" ]] && swift_dirs+=("ios/$dir")
  done
  echo "swift-format $(xcrun swift-format --version)"
  if [[ "$MODE" == format ]]; then
    (cd "$ROOT" && xcrun swift-format format --in-place --recursive --parallel \
      --configuration ios/.swift-format "${swift_dirs[@]}")
    exit 0
  fi
  (cd "$ROOT" && xcrun swift-format lint --strict --recursive --parallel \
    --configuration ios/.swift-format "${swift_dirs[@]}")
fi
command -v xcodegen >/dev/null || { echo "xcodegen is missing (brew install xcodegen)." >&2; exit 1; }

set -a
eval "$("$ROOT/scripts/version.sh" --env)"
set +a
echo "version $ICHOR_VERSION (build $ICHOR_BUILD_NUMBER)"

# The watch targets use IchorCore only (no Go core, no licenses): generate (always, so a stale
# project cannot lack the IchorWatch scheme) and build that scheme, which builds its complication.
if [[ "$MODE" == watch ]]; then
  cd "$ROOT/ios"
  xcodegen generate
  xcodebuild -project Ichor.xcodeproj -scheme IchorWatch -derivedDataPath build \
    MARKETING_VERSION="$ICHOR_VERSION_BASE" CURRENT_PROJECT_VERSION="$ICHOR_BUILD_NUMBER" \
    -skipPackagePluginValidation -skipMacroValidation \
    CODE_SIGNING_ALLOWED=NO -configuration Debug -destination 'generic/platform=watchOS Simulator' build
  exit 0
fi
# Release notes bundled in the app, for the "what's new" shown after an update.
python3 "$ROOT/scripts/changelog.py" --limit 30 -o "$ROOT/ios/Ichor/changelog.json"

# gomobile/gobind versions are pinned by the tool directive in go/go.mod.
cd "$ROOT/go"
export GOBIN="$ROOT/.cache/gobin" PATH="$ROOT/.cache/gobin:$PATH"
go install tool
rm -rf "$ROOT/ios/Frameworks/Ichorgo.xcframework"
gomobile bind -target=ios,iossimulator -iosversion=17.0 -ldflags="-s -w" \
  -o "$ROOT/ios/Frameworks/Ichorgo.xcframework" ./ichorgo

# The on-device model engine: llama.cpp's xcframework (Metal) at the release pinned in
# native/llama-cpp.env, built once per commit into .cache/ (CI caches it), device + simulator.
# The source is fetched even when the framework is cached: the licenses below read it.
# shellcheck source=../native/llama-cpp.env
source "$ROOT/native/llama-cpp.env"
llama_src="$("$ROOT/scripts/fetch-llama.sh")"
llama_fw="$ROOT/.cache/llama-xcframework-$LLAMA_COMMIT/llama.xcframework"
if [[ ! -d "$llama_fw" ]]; then
  (cd "$llama_src" && ./build-xcframework.sh ios-sim ios-device)
  mkdir -p "$(dirname "$llama_fw")"
  cp -R "$llama_src/build-apple/llama.xcframework" "$llama_fw"
fi
rm -rf "$ROOT/ios/Frameworks/llama.xcframework"
cp -R "$llama_fw" "$ROOT/ios/Frameworks/llama.xcframework"

python3 "$ROOT/scripts/check-translations.py"
if [[ "${ICHOR_SKIP_TESTS:-0}" == 1 ]]; then
  echo "ICHOR_SKIP_TESTS=1: skipping the IchorCore tests"
else
  (cd "$ROOT/ios/IchorCore" && swift test)
fi

cd "$ROOT/ios"
xcodegen generate
# iOS only accepts numeric x.y.z marketing versions; the full string is in the IPA name.
common=(
  -project Ichor.xcodeproj -scheme Ichor -derivedDataPath build
  MARKETING_VERSION="$ICHOR_VERSION_BASE" CURRENT_PROJECT_VERSION="$ICHOR_BUILD_NUMBER"
  # SwiftTerm ships a build-tool plugin; CI cannot click "Trust & Enable".
  -skipPackagePluginValidation -skipMacroValidation
)
unsigned=(CODE_SIGNING_ALLOWED=NO)

# Licenses of everything linked in (About › Open-source licenses): the Swift packages are only
# checked out once resolved, and the generated file is only part of the project once XcodeGen
# sees it, hence the second generate.
xcodebuild -project Ichor.xcodeproj -scheme Ichor -derivedDataPath build -resolvePackageDependencies \
  -skipPackagePluginValidation -skipMacroValidation >/dev/null
python3 "$ROOT/scripts/go-licenses.py" --ios Ichor/licenses.json \
  --swift-checkouts build/SourcePackages/checkouts \
  --swift-resolved Ichor.xcodeproj/project.xcworkspace/xcshareddata/swiftpm/Package.resolved
xcodegen generate
# XcodeGen rewrites the entitlements file on each generate, so this comes after the last one.
if [[ "${ICHOR_IOS_MULTICAST:-}" == 1 ]]; then
  /usr/libexec/PlistBuddy -c "Add :com.apple.developer.networking.multicast bool true" Ichor/Ichor.entitlements
  echo "entitlement com.apple.developer.networking.multicast added"
fi
if [[ -n "${ICHOR_IOS_EXPORT_CODE:-}" ]]; then
  /usr/libexec/PlistBuddy -c "Add :ITSEncryptionExportComplianceCode string $ICHOR_IOS_EXPORT_CODE" Ichor/Info.plist
fi

case "$MODE" in
  test)
    # The Debug simulator build, then the demo-mode smoke test (IchorUITests) on it, from one
    # build: build-for-testing compiles the app and the test bundle once, test-without-building
    # runs them. The simulator is the runner's first iPhone, picked from simctl since the
    # image's models change. Signed ad hoc, not unsigned: an unsigned app has no keychain
    # entitlement in the simulator, so storing the demo's config fails
    # (errSecMissingEntitlement, -34018); the simulator needs no team or profile for it. The
    # unsigned build stays checked by `ipa`. The result bundle is build/ui-tests.xcresult
    # (uploaded by ios.yml on failure).
    sim=$(xcrun simctl list devices available | awk -F '[()]' '/iPhone/ { print $2; exit }')
    [[ -n "$sim" ]] || { echo "no iPhone simulator available" >&2; exit 1; }
    adhoc=(CODE_SIGNING_ALLOWED=YES CODE_SIGN_IDENTITY=- CODE_SIGN_STYLE=Manual DEVELOPMENT_TEAM= PROVISIONING_PROFILE_SPECIFIER=)
    xcodebuild "${common[@]}" "${adhoc[@]}" -configuration Debug -destination "id=$sim" build-for-testing
    rm -rf build/ui-tests.xcresult
    xcodebuild "${common[@]}" "${adhoc[@]}" -configuration Debug -destination "id=$sim" \
      -only-testing:IchorUITests -resultBundlePath build/ui-tests.xcresult test-without-building
    ;;
  screenshots)
    # The screenshot walk (IchorUITests/DemoScreenshotTests) in each appearance, from one build:
    # result bundles build/screenshots-dark.xcresult and -light, read by scripts/ios-screenshots.sh.
    # The same simulator and signing as `test`. The simulator is booted first, since appearance and
    # status bar only apply to a booted one, and the appearance takes effect on the next launch.
    # No parallel testing: a clone of the simulator would have neither.
    sim=$(xcrun simctl list devices available | awk -F '[()]' '/iPhone/ { print $2; exit }')
    [[ -n "$sim" ]] || { echo "no iPhone simulator available" >&2; exit 1; }
    adhoc=(CODE_SIGNING_ALLOWED=YES CODE_SIGN_IDENTITY=- CODE_SIGN_STYLE=Manual DEVELOPMENT_TEAM= PROVISIONING_PROFILE_SPECIFIER=)
    xcodebuild "${common[@]}" "${adhoc[@]}" -configuration Debug -destination "id=$sim" build-for-testing
    xcrun simctl bootstatus "$sim" -b
    for appearance in dark light; do
      xcrun simctl ui "$sim" appearance "$appearance"
      xcrun simctl status_bar "$sim" override --time 9:41 --batteryState charged --batteryLevel 100 \
        --wifiBars 3 --cellularBars 4
      rm -rf "build/screenshots-$appearance.xcresult"
      TEST_RUNNER_ICHOR_SCREENSHOTS=1 xcodebuild "${common[@]}" "${adhoc[@]}" -configuration Debug \
        -destination "id=$sim" -parallel-testing-enabled NO -only-testing:IchorUITests/DemoScreenshotTests \
        -resultBundlePath "build/screenshots-$appearance.xcresult" test-without-building
    done
    ;;
  ipa)
    xcodebuild "${common[@]}" "${unsigned[@]}" -configuration Release -destination 'generic/platform=iOS' build
    rm -rf build/ipa && mkdir -p build/ipa/Payload
    cp -R build/Build/Products/Release-iphoneos/Ichor.app build/ipa/Payload/
    # App Store Connect rejects uploads without them (ITMS-91053/91061); fail here first.
    for manifest in Ichor.app/PrivacyInfo.xcprivacy Ichor.app/PlugIns/TalosWidget.appex/PrivacyInfo.xcprivacy \
      Ichor.app/PlugIns/IchorShare.appex/PrivacyInfo.xcprivacy; do
      [[ -f "build/ipa/Payload/$manifest" ]] || { echo "missing privacy manifest: $manifest" >&2; exit 1; }
    done
    ipa="ichor-v${ICHOR_VERSION//+/-}-unsigned.ipa"
    (cd build/ipa && zip -qry "../$ipa" Payload)
    echo "$ROOT/ios/build/$ipa"
    ;;
  appstore)
    auth=(
      -allowProvisioningUpdates -authenticationKeyPath "$ASC_KEY_PATH"
      -authenticationKeyID "$ASC_KEY_ID" -authenticationKeyIssuerID "$ASC_ISSUER_ID"
    )
    rm -rf build/Ichor.xcarchive build/appstore
    # $(inherited) is expanded by xcodebuild, not the shell.
    # shellcheck disable=SC2016
    xcodebuild "${common[@]}" "${auth[@]}" -configuration Release -destination 'generic/platform=iOS' \
      -archivePath build/Ichor.xcarchive \
      DEVELOPMENT_TEAM="$ICHOR_IOS_TEAM_ID" CODE_SIGN_STYLE=Automatic \
      SWIFT_ACTIVE_COMPILATION_CONDITIONS='$(inherited) APP_STORE' \
      archive
    # destination upload: the export goes straight to App Store Connect, then TestFlight.
    cat > build/ExportOptions.plist <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>method</key><string>app-store-connect</string>
  <key>destination</key><string>upload</string>
  <key>teamID</key><string>$ICHOR_IOS_TEAM_ID</string>
  <key>signingStyle</key><string>automatic</string>
  <key>manageAppVersionAndBuildNumber</key><false/>
  <key>uploadSymbols</key><true/>
</dict>
</plist>
PLIST
    xcodebuild -exportArchive "${auth[@]}" -archivePath build/Ichor.xcarchive \
      -exportOptionsPlist build/ExportOptions.plist -exportPath build/appstore
    ;;
esac

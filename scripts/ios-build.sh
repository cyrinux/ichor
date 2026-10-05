#!/usr/bin/env bash
# Builds the iOS app on macOS (Xcode + xcodegen + Go):
#   1. Ichorgo.xcframework from the Go core (gomobile, device + simulator);
#   2. the core Swift package tests;
#   3. the Xcode project (XcodeGen) and the app.
#
#   scripts/ios-build.sh test   # core tests + simulator build (CI on every push)
#   scripts/ios-build.sh ipa    # unsigned Release build packaged as an IPA (default)
#   scripts/ios-build.sh appstore  # App Store build, signed and uploaded to App Store Connect
#
# The unsigned IPA can be re-signed and installed with your Apple ID by Sideloadly or
# AltStore. Set ICHOR_IOS_TEAM_ID and drop CODE_SIGNING_ALLOWED=NO to sign in Xcode instead.
#
# appstore compiles with APP_STORE (no donation links, see Distribution.swift) and lets Xcode
# sign it (automatic signing, cloud-managed distribution certificate) with an App Store
# Connect API key, then uploads it (TestFlight). It needs ICHOR_IOS_TEAM_ID, ASC_KEY_PATH (the
# AuthKey_<id>.p8 file), ASC_KEY_ID and ASC_ISSUER_ID.
set -euo pipefail

MODE="${1:-ipa}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
case "$MODE" in
  test|ipa) ;;
  appstore)
    : "${ICHOR_IOS_TEAM_ID:?ICHOR_IOS_TEAM_ID is not set}"
    : "${ASC_KEY_PATH:?ASC_KEY_PATH is not set (App Store Connect API key, AuthKey_<id>.p8)}"
    : "${ASC_KEY_ID:?ASC_KEY_ID is not set}"
    : "${ASC_ISSUER_ID:?ASC_ISSUER_ID is not set}"
    ;;
  *)
    echo "usage: $0 [test|ipa|appstore]" >&2
    exit 2
    ;;
esac

[[ "$(uname -s)" == Darwin ]] || { echo "iOS builds need macOS with Xcode." >&2; exit 1; }
command -v xcodegen >/dev/null || { echo "xcodegen is missing (brew install xcodegen)." >&2; exit 1; }

set -a
eval "$("$ROOT/scripts/version.sh" --env)"
set +a
echo "version $ICHOR_VERSION (build $ICHOR_BUILD_NUMBER)"
# Release notes bundled in the app, for the "what's new" shown after an update.
python3 "$ROOT/scripts/changelog.py" --limit 30 -o "$ROOT/ios/Ichor/changelog.json"

# gomobile/gobind versions are pinned by the tool directive in go/go.mod.
cd "$ROOT/go"
export GOBIN="$ROOT/.cache/gobin" PATH="$ROOT/.cache/gobin:$PATH"
go install tool
rm -rf "$ROOT/ios/Frameworks/Ichorgo.xcframework"
gomobile bind -target=ios,iossimulator -iosversion=17.0 -ldflags="-s -w" \
  -o "$ROOT/ios/Frameworks/Ichorgo.xcframework" ./ichorgo

python3 "$ROOT/scripts/check-translations.py"
(cd "$ROOT/ios/IchorCore" && swift test)

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

case "$MODE" in
  test)
    xcodebuild "${common[@]}" "${unsigned[@]}" -configuration Debug -destination 'generic/platform=iOS Simulator' build
    ;;
  ipa)
    xcodebuild "${common[@]}" "${unsigned[@]}" -configuration Release -destination 'generic/platform=iOS' build
    rm -rf build/ipa && mkdir -p build/ipa/Payload
    cp -R build/Build/Products/Release-iphoneos/Ichor.app build/ipa/Payload/
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

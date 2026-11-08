// swift-tools-version:6.0
// Pure-Foundation logic shared by the iOS app and its tests (also runnable on Linux,
// see test-linux.sh): JSON models, formatting, lock state, power requests. The Apple Watch
// app and its complication use it too (WatchGlance.swift).
// Swift 6 language mode: complete concurrency checking (test-linux.sh passes the same flags).
import PackageDescription

let package = Package(
    name: "IchorCore",
    platforms: [.iOS(.v17), .macOS(.v14), .watchOS(.v10)],
    products: [.library(name: "IchorCore", targets: ["IchorCore"])],
    targets: [
        .target(name: "IchorCore"),
        .testTarget(name: "IchorCoreTests", dependencies: ["IchorCore"]),
    ],
    swiftLanguageModes: [.v6]
)

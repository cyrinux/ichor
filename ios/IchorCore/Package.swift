// swift-tools-version:5.9
// Pure-Foundation logic shared by the iOS app and its tests (also runnable on Linux,
// see test-linux.sh): JSON models, formatting, lock state, power requests.
import PackageDescription

let package = Package(
    name: "IchorCore",
    platforms: [.iOS(.v17), .macOS(.v14)],
    products: [.library(name: "IchorCore", targets: ["IchorCore"])],
    targets: [
        .target(name: "IchorCore"),
        .testTarget(name: "IchorCoreTests", dependencies: ["IchorCore"]),
    ]
)

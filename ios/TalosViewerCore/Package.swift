// swift-tools-version:5.9
// Pure-Foundation logic shared by the iOS app and its tests (also runnable on Linux,
// see test-linux.sh): JSON models, formatting, lock state, power requests.
import PackageDescription

let package = Package(
    name: "TalosViewerCore",
    platforms: [.iOS(.v17), .macOS(.v14)],
    products: [.library(name: "TalosViewerCore", targets: ["TalosViewerCore"])],
    targets: [
        .target(name: "TalosViewerCore"),
        .testTarget(name: "TalosViewerCoreTests", dependencies: ["TalosViewerCore"]),
    ]
)

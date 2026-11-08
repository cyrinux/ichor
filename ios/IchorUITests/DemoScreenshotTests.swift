import XCTest

/// The website's iPhone screenshots, taken from the demo on a simulator (scripts/ios-build.sh
/// screenshots, then scripts/ios-screenshots.sh): the twin of Android's DemoScreenshots, with its
/// names and settle times. Skipped unless the run asks for it, so the smoke run stays the same.
/// Each screen is an attachment named after it; the appearance and the status bar are set on the
/// simulator before the run. The screens wait for an identifier (the app's SmokeID), then settle.
final class DemoScreenshotTests: XCTestCase {
    private static let featureSettle: TimeInterval = 5

    /// Names match scripts/ios-screenshots.sh. castai opens the data services screen.
    private static let features: [(name: String, anchor: String)] = [
        ("plugins", WalkID.screen("plugins")),
        ("gitops", WalkID.screen("gitops")),
        ("capacity", WalkID.screen("capacity")),
        ("argocd-diff", WalkID.screen("argocd-diff")),
        ("argocd", WalkID.screen("argocd")),
        ("metrics", WalkID.screen("metrics")),
        ("data-services", WalkID.screen("data-services")),
        ("castai", WalkID.screen("data-services")),
        ("cgroups", WalkID.screen("cgroups")),
        ("apps", WalkID.screen("apps")),
        ("workloads", WalkID.screen("workloads")),
        ("pods", WalkID.screen("pods")),
        ("cronjobs", WalkID.screen("cronjobs")),
        ("netperf", WalkID.screen("netperf")),
        ("drift", WalkID.screen("drift")),
        ("import", WalkID.screen("import")),
    ]

    override func setUpWithError() throws {
        continueAfterFailure = false
        try XCTSkipUnless(
            ProcessInfo.processInfo.environment["ICHOR_SCREENSHOTS"] == "1",
            "Only for the screenshots run (TEST_RUNNER_ICHOR_SCREENSHOTS=1)")
    }

    /// The listing's seven screens (ListingWalk), named as on the website.
    @MainActor
    func testCaptureTheDemo() {
        captureListingScreens()
    }

    /// The screens of the website's feature cards, each opened directly from the demo.
    @MainActor
    func testCaptureTheFeatures() {
        for feature in Self.features {
            waitFor(feature.anchor, in: openDemo("demo", screen: feature.name))
            captureScreen("feature-\(feature.name)", settle: Self.featureSettle)
        }
        waitFor(WalkID.kubeHome, in: openDemo("demo-kube"))
        captureScreen("feature-kube-home", settle: Self.featureSettle)
    }
}

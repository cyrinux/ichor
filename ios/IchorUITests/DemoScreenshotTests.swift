import XCTest

/// The website's iPhone screenshots, taken from the demo on a simulator (scripts/ios-build.sh
/// screenshots, then scripts/ios-screenshots.sh): the twin of Android's DemoScreenshots, with its
/// names and settle times. Skipped unless the run asks for it, so the smoke run stays the same.
/// Each screen is an attachment named after it; the appearance and the status bar are set on the
/// simulator before the run. The screens wait for an identifier (the app's SmokeID), then settle.
final class DemoScreenshotTests: XCTestCase {
    private enum ID {
        static let overview = "smoke-overview"
        static let logLines = "smoke-log-lines"
        static let nodeServices = "smoke-node-services"
        static let events = "smoke-events"
        static let graphs = "smoke-graphs"
        static let captureStart = "smoke-capture-start"
        static let upgradePlan = "smoke-upgrade-plan"
        static let kubeHome = "smoke-kube-home"
        static func screen(_ name: String) -> String { "smoke-screen-\(name)" }
    }

    private enum Settle {
        static let standard: TimeInterval = 2.5
        /// Most of the live graphs' three minutes, so that they draw more than a sliver.
        static let graphs: TimeInterval = 150
        /// A screenful of sample packets.
        static let capture: TimeInterval = 8
        /// The releases (from the network), then the plan of the newest.
        static let upgrade: TimeInterval = 8
        static let feature: TimeInterval = 5
    }

    /// The images show the app as after its demo notice is closed (DemoNoticeSection).
    private static let closedNotice = ["-overview.demoNoticeDismissed", "YES"]

    /// Names match scripts/ios-screenshots.sh. castai opens the data services screen.
    private static let features: [(name: String, anchor: String)] = [
        ("plugins", ID.screen("plugins")),
        ("gitops", ID.screen("gitops")),
        ("capacity", ID.screen("capacity")),
        ("argocd-diff", ID.screen("argocd-diff")),
        ("argocd", ID.screen("argocd")),
        ("metrics", ID.screen("metrics")),
        ("data-services", ID.screen("data-services")),
        ("castai", ID.screen("data-services")),
        ("cgroups", ID.screen("cgroups")),
        ("apps", ID.screen("apps")),
        ("workloads", ID.screen("workloads")),
        ("pods", ID.screen("pods")),
        ("cronjobs", ID.screen("cronjobs")),
        ("netperf", ID.screen("netperf")),
        ("drift", ID.screen("drift")),
        ("import", ID.screen("import")),
    ]

    override func setUpWithError() throws {
        continueAfterFailure = false
        try XCTSkipUnless(
            ProcessInfo.processInfo.environment["ICHOR_SCREENSHOTS"] == "1",
            "Only for the screenshots run (TEST_RUNNER_ICHOR_SCREENSHOTS=1)")
    }

    @MainActor
    func testCaptureTheDemo() {
        waitFor(ID.overview, in: open("demo"))
        capture("overview", settle: Settle.standard)

        let shots: [(name: String, anchor: String, settle: TimeInterval)] = [
            ("node-services", ID.nodeServices, Settle.standard),
            ("logs", ID.logLines, Settle.standard),
            ("events", ID.events, Settle.standard),
            ("graphs", ID.graphs, Settle.graphs),
            ("upgrade", ID.upgradePlan, Settle.upgrade),
        ]
        for shot in shots {
            waitFor(shot.anchor, in: open("demo", screen: shot.name))
            capture(shot.name, settle: shot.settle)
        }

        // The start button is at the end of the options, and enabled once an interface is known.
        let app = open("demo", screen: "capture")
        revealEnabled(ID.captureStart, in: app).tap()
        capture("capture", settle: Settle.capture)
    }

    /// The screens of the website's feature cards, each opened directly from the demo.
    @MainActor
    func testCaptureTheFeatures() {
        for feature in Self.features {
            waitFor(feature.anchor, in: open("demo", screen: feature.name))
            capture("feature-\(feature.name)", settle: Settle.feature)
        }
        waitFor(ID.kubeHome, in: open("demo-kube"))
        capture("feature-kube-home", settle: Settle.feature)
    }

    /// Swipes up until `id` lies inside the window, enabled, on two looks in a row: the options load
    /// late and can push a Form row back off screen, where the list drops it. Through snapshots,
    /// which answer for a missing element: frame, isHittable or tap fail the test on one.
    @MainActor
    private func revealEnabled(_ id: String, in app: XCUIApplication) -> XCUIElement {
        let found = element(id, in: app)
        let window = app.windows.firstMatch.frame
        var steady = false
        for _ in 0..<30 {
            let shot = try? found.snapshot()
            if let shot, !shot.frame.isEmpty, window.contains(shot.frame) {
                if shot.isEnabled, steady { return found }
                steady = shot.isEnabled
                Thread.sleep(forTimeInterval: Settle.standard)
            } else {
                steady = false
                app.swipeUp()
            }
        }
        XCTFail("\(id) not reachable and enabled; on screen:\n\(app.debugDescription)")
        return found
    }

    /// The demo alone on the phone, its notice closed: the same picture in every run.
    @MainActor
    private func open(_ demo: String, screen: String? = nil) -> XCUIApplication {
        launch(demo, screen: screen, alone: true, arguments: Self.closedNotice)
    }

    /// Lets the screen load its demo data, then attaches the whole screen as `name`.
    @MainActor
    private func capture(_ name: String, settle: TimeInterval) {
        Thread.sleep(forTimeInterval: settle)
        let attachment = XCTAttachment(screenshot: XCUIScreen.main.screenshot(), quality: .original)
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }
}

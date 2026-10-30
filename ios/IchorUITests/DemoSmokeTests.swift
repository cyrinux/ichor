import XCTest

/// The demo cluster walked on a simulator (CYR-473, the iOS twin of Android's DemoSmokeTest):
/// the crash-on-launch and blank-screen regressions the IchorCore tests cannot see, since the
/// app's views only compile on macOS CI. The demo is offline; `ICHOR_UI_TEST` adds it at launch
/// (UITestLaunch). Every step waits for an accessibility identifier, never for a fixed time.
final class DemoSmokeTests: XCTestCase {
    // The app's SmokeID (Ichor/App/UITestLaunch.swift): a UI test cannot import the app.
    private enum ID {
        static let node = "smoke-node"
        static let kernelLog = "smoke-node-kernel-log"
        static let containers = "smoke-node-containers"
        static let containerRow = "smoke-container-row"
        static let settings = "smoke-settings"
        static let settingsScreen = "smoke-settings-screen"
        static let logLines = "smoke-log-lines"
        static let kubeHome = "smoke-kube-home"
    }

    /// A cold simulator: the first screen of a fresh install can take a while, longer with four
    /// tests on one simulator (the Kubernetes home once showed just after a 30 s wait).
    private let timeout: TimeInterval = 60

    override func setUp() {
        continueAfterFailure = false
    }

    func testTheTalosDemoOpensANodeAndItsKernelLog() {
        let app = launch("demo")

        // The overview is a lazy list: the nodes may be below the fold.
        scrollTo(ID.node, in: app).tap()
        // The node hub's list, once it shows.
        scrollTo(ID.kernelLog, in: app).tap()

        waitFor(ID.logLines, in: app)
    }

    func testTheTalosDemoListsANodesContainersAndOpensALog() {
        let app = launch("demo")

        scrollTo(ID.node, in: app).tap()
        scrollTo(ID.containers, in: app).tap()
        // Wait on the rows, not on the list's loading state.
        waitFor(ID.containerRow, in: app).tap()

        waitFor(ID.logLines, in: app)
    }

    func testTheOverviewOpensSettings() {
        // Settings is in the ⋯ menu by default, and on a phone iOS folds the toolbar into its own
        // overflow, whose items lose their identifiers: the bar is arranged with Settings as its
        // only icon (the overview's @AppStorage("overview.bar"), "icons|menu").
        let app = launch("demo", arguments: ["-overview.bar", "SETTINGS|"])

        // The bar's button shows before the overview has finished loading, and a tap while the
        // screen is still rebuilding opens nothing: tap again until Settings shows.
        tap(ID.settings, until: ID.settingsScreen, in: app)
    }

    func testTheKubernetesDemoOpensItsHome() {
        let app = launch("demo-kube")
        waitFor(ID.kubeHome, in: app)
    }

    private func launch(_ demo: String, arguments: [String] = []) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchEnvironment["ICHOR_UI_TEST"] = demo
        app.launchArguments += arguments
        app.launch()
        return app
    }

    private func element(_ id: String, in app: XCUIApplication) -> XCUIElement {
        app.descendants(matching: .any)[id].firstMatch
    }

    /// Waits for `id`; on a timeout the failure carries the screen's element tree.
    @discardableResult
    private func waitFor(_ id: String, in app: XCUIApplication) -> XCUIElement {
        let found = element(id, in: app)
        if !found.waitForExistence(timeout: timeout) {
            XCTFail("\(id) not found; on screen:\n\(app.debugDescription)")
        }
        return found
    }

    /// Taps `id` until `target` shows, at most three times: a tap that lands while the screen is
    /// rebuilding opens nothing. Fails with the screen's element tree when `target` never shows.
    private func tap(_ id: String, until target: String, in app: XCUIApplication) {
        let button = waitFor(id, in: app)
        let shown = element(target, in: app)
        for _ in 0..<3 {
            if button.isHittable { button.tap() }
            if shown.waitForExistence(timeout: timeout / 3) { return }
        }
        XCTFail("\(target) not shown after tapping \(id); on screen:\n\(app.debugDescription)")
    }

    /// Waits for the first screen to settle, then swipes up until `id` is on screen: a List
    /// only holds the rows that show.
    private func scrollTo(_ id: String, in app: XCUIApplication) -> XCUIElement {
        let found = element(id, in: app)
        if found.waitForExistence(timeout: timeout), found.isHittable { return found }
        for _ in 0..<8 where !(found.exists && found.isHittable) {
            app.swipeUp()
        }
        if !(found.exists && found.isHittable) {
            XCTFail("\(id) not reachable; on screen:\n\(app.debugDescription)")
        }
        return found
    }
}

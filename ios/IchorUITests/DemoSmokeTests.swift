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
        static let actionsMore = "smoke-actions-more"
        static let settings = "smoke-settings"
        static let settingsScreen = "smoke-settings-screen"
        static let logLines = "smoke-log-lines"
        static let kubeHome = "smoke-kube-home"
    }

    /// A cold simulator: the first screen of a fresh install can take a while.
    private let timeout: TimeInterval = 30

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
        let app = launch("demo")

        // Settings is a toolbar icon or, by default, an item behind the ⋯ menu.
        waitFor(ID.actionsMore, in: app)
        if !element(ID.settings, in: app).exists {
            element(ID.actionsMore, in: app).tap()
        }
        waitFor(ID.settings, in: app).tap()

        waitFor(ID.settingsScreen, in: app)
    }

    func testTheKubernetesDemoOpensItsHome() {
        let app = launch("demo-kube")
        waitFor(ID.kubeHome, in: app)
    }

    private func launch(_ demo: String) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchEnvironment["ICHOR_UI_TEST"] = demo
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

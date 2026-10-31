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

    override func setUp() {
        continueAfterFailure = false
    }

    @MainActor
    func testTheTalosDemoOpensANodeAndItsKernelLog() {
        let app = launch("demo")

        // The overview is a lazy list: the nodes may be below the fold.
        scrollTo(ID.node, in: app).tap()
        // The node hub's list, once it shows.
        scrollTo(ID.kernelLog, in: app).tap()

        waitFor(ID.logLines, in: app)
    }

    @MainActor
    func testTheTalosDemoListsANodesContainersAndOpensALog() {
        let app = launch("demo")

        scrollTo(ID.node, in: app).tap()
        scrollTo(ID.containers, in: app).tap()
        // Wait on the rows, not on the list's loading state.
        waitFor(ID.containerRow, in: app).tap()

        waitFor(ID.logLines, in: app)
    }

    @MainActor
    func testTheOverviewOpensSettings() {
        // Settings is in the ⋯ menu by default, and on a phone iOS folds the toolbar into its own
        // overflow, whose items lose their identifiers: the bar is arranged with Settings as its
        // only icon (the overview's @AppStorage("overview.bar"), "icons|menu").
        let app = launch("demo", arguments: ["-overview.bar", "SETTINGS|"])

        // The bar's button shows before the overview has finished loading, and a tap while the
        // screen is still rebuilding opens nothing: tap again until Settings shows.
        tap(ID.settings, until: ID.settingsScreen, in: app)
    }

    @MainActor
    func testTheKubernetesDemoOpensItsHome() {
        let app = launch("demo-kube")
        waitFor(ID.kubeHome, in: app)
    }
}

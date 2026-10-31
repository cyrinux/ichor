import XCTest

/// A cold simulator: the first screen of a fresh install can take a while, longer with four
/// tests on one simulator (the Kubernetes home once showed just after a 30 s wait).
let demoWalkTimeout: TimeInterval = 60

/// What the demo walks share (DemoSmokeTests, DemoScreenshotTests). Every step waits for an
/// accessibility identifier, the app's SmokeID: a UI test cannot import the app.
extension XCTestCase {
    /// Launches the app on the demo; `screen` opens one of its screens (UITestScreen).
    @MainActor
    func launch(_ demo: String, screen: String? = nil, arguments: [String] = []) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchEnvironment["ICHOR_UI_TEST"] = demo
        if let screen { app.launchEnvironment["ICHOR_UI_TEST_SCREEN"] = screen }
        app.launchArguments += arguments
        app.launch()
        return app
    }

    @MainActor
    func element(_ id: String, in app: XCUIApplication) -> XCUIElement {
        app.descendants(matching: .any)[id].firstMatch
    }

    /// Waits for `id`; on a timeout the failure carries the screen's element tree.
    @MainActor
    @discardableResult
    func waitFor(_ id: String, in app: XCUIApplication) -> XCUIElement {
        let found = element(id, in: app)
        if !found.waitForExistence(timeout: demoWalkTimeout) {
            XCTFail("\(id) not found; on screen:\n\(app.debugDescription)")
        }
        return found
    }

    /// Taps `id` until `target` shows, at most three times: a tap that lands while the screen is
    /// rebuilding opens nothing. Fails with the screen's element tree when `target` never shows.
    @MainActor
    func tap(_ id: String, until target: String, in app: XCUIApplication) {
        let button = waitFor(id, in: app)
        let shown = element(target, in: app)
        for _ in 0..<3 {
            if button.isHittable { button.tap() }
            if shown.waitForExistence(timeout: demoWalkTimeout / 3) { return }
        }
        XCTFail("\(target) not shown after tapping \(id); on screen:\n\(app.debugDescription)")
    }

    /// Waits up to `settle` for the first screen, then swipes up until `id` is on screen: a List
    /// only holds the rows that show.
    @MainActor
    func scrollTo(_ id: String, in app: XCUIApplication, settle: TimeInterval = demoWalkTimeout) -> XCUIElement {
        let found = element(id, in: app)
        if found.waitForExistence(timeout: settle), found.isHittable { return found }
        for _ in 0..<8 where !(found.exists && found.isHittable) {
            app.swipeUp()
        }
        if !(found.exists && found.isHittable) {
            XCTFail("\(id) not reachable; on screen:\n\(app.debugDescription)")
        }
        return found
    }
}

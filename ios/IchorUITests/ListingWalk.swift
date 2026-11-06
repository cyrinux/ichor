import XCTest

/// The identifiers the screenshot walks wait for: the app's SmokeID, which a UI test cannot import.
enum WalkID {
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

/// How long each screen gets once its identifier shows, as Android's DemoWalk.
enum WalkSettle {
    static let standard: TimeInterval = 2.5
    /// Most of the live graphs' three minutes, so that they draw more than a sliver.
    static let graphs: TimeInterval = 150
    /// A screenful of sample packets.
    static let capture: TimeInterval = 8
    /// The releases (from the network), then the plan of the newest.
    static let upgrade: TimeInterval = 8
}

/// The seven screens of the store listings, walked by DemoScreenshotTests (the website) and
/// StoreScreenshotTests (the App Store, once per language).
enum ListingWalk {
    /// The listings' order, as scripts/store-screenshots.sh: the App Store set is N_NAME after it.
    static let shots = ["overview", "node-services", "logs", "events", "graphs", "capture", "upgrade"]

    /// The images show the app as after its demo notice is closed (DemoNoticeSection).
    static let closedNotice = ["-overview.demoNoticeDismissed", "YES"]

    /// The listing number of a subject of `shots`, from 1.
    static func number(of subject: String) -> Int {
        (shots.firstIndex(of: subject) ?? shots.count) + 1
    }
}

extension XCTestCase {
    /// Opens each listing screen directly on the demo and attaches it as `name(subject)`.
    /// `arguments` are added to every launch (a language, for the App Store set).
    @MainActor
    func captureListingScreens(arguments: [String] = [], name: (String) -> String = { $0 }) {
        waitFor(WalkID.overview, in: openDemo("demo", arguments: arguments))
        captureScreen(name("overview"), settle: WalkSettle.standard)

        let shots: [(name: String, anchor: String, settle: TimeInterval)] = [
            ("node-services", WalkID.nodeServices, WalkSettle.standard),
            ("logs", WalkID.logLines, WalkSettle.standard),
            ("events", WalkID.events, WalkSettle.standard),
            ("graphs", WalkID.graphs, WalkSettle.graphs),
            ("upgrade", WalkID.upgradePlan, WalkSettle.upgrade),
        ]
        for shot in shots {
            waitFor(shot.anchor, in: openDemo("demo", screen: shot.name, arguments: arguments))
            captureScreen(name(shot.name), settle: shot.settle)
        }

        // The start button is at the end of the options, and enabled once an interface is known.
        let app = openDemo("demo", screen: "capture", arguments: arguments)
        revealEnabled(WalkID.captureStart, in: app).tap()
        captureScreen(name("capture"), settle: WalkSettle.capture)
    }

    /// The demo alone on the phone, its notice closed: the same picture in every run.
    @MainActor
    func openDemo(_ demo: String, screen: String? = nil, arguments: [String] = []) -> XCUIApplication {
        launch(demo, screen: screen, alone: true, arguments: ListingWalk.closedNotice + arguments)
    }

    /// Lets the screen load its demo data, then attaches the whole screen as `name`.
    @MainActor
    func captureScreen(_ name: String, settle: TimeInterval) {
        Thread.sleep(forTimeInterval: settle)
        let attachment = XCTAttachment(screenshot: XCUIScreen.main.screenshot(), quality: .original)
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }

    /// Swipes up until `id` lies inside the window, enabled, on two looks in a row: the options load
    /// late and can push a Form row back off screen, where the list drops it. Through snapshots,
    /// which answer for a missing element: frame, isHittable or tap fail the test on one.
    @MainActor
    func revealEnabled(_ id: String, in app: XCUIApplication) -> XCUIElement {
        let found = element(id, in: app)
        let window = app.windows.firstMatch.frame
        var steady = false
        for _ in 0..<30 {
            let shot = try? found.snapshot()
            if let shot, !shot.frame.isEmpty, window.contains(shot.frame) {
                if shot.isEnabled, steady { return found }
                steady = shot.isEnabled
                Thread.sleep(forTimeInterval: WalkSettle.standard)
            } else {
                steady = false
                app.swipeUp()
            }
        }
        XCTFail("\(id) not reachable and enabled; on screen:\n\(app.debugDescription)")
        return found
    }
}

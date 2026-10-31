import Foundation
import Observation
import SwiftUI

/// The anchors the demo-mode smoke test (IchorUITests/DemoSmokeTests.swift) waits for and taps,
/// the same names as Android's SmokeTags.kt. Accessibility identifiers are not shown or read out
/// (VoiceOver reads the labels); when a screen moves an anchor, move its identifier, not the test.
enum SmokeID {
    /// An overview node row.
    static let node = "smoke-node"
    /// The node hub's "Kernel log" entry.
    static let kernelLog = "smoke-node-kernel-log"
    /// The node hub's "Containers" entry.
    static let containers = "smoke-node-containers"
    /// A container row of a node's containers list (it opens the container's log).
    static let containerRow = "smoke-container-row"
    /// The overview's Settings action: its icon, or its item in the ⋯ menu.
    static let settings = "smoke-settings"
    /// The Settings screen's list of pages.
    static let settingsScreen = "smoke-settings-screen"
    /// The lines of a logs screen.
    static let logLines = "smoke-log-lines"
    /// The Kubernetes home of a cluster added from a kubeconfig.
    static let kubeHome = "smoke-kube-home"
    /// Why the launch could not add the demo (UITestLaunch.failure), in the test's element tree.
    static let launchError = "smoke-launch-error"
    /// The overview's list, once it has loaded (the nodes may be below the fold).
    static let overview = "smoke-overview"
    // The screens of the screenshot walk (IchorUITests/DemoScreenshotTests.swift), on what shows
    // once each has loaded.
    /// A node's services list.
    static let nodeServices = "smoke-node-services"
    /// A row of the events timeline.
    static let events = "smoke-events"
    /// The first chart of a node's Live tab.
    static let graphs = "smoke-graphs"
    /// The capture's start button (enabled once an interface is known).
    static let captureStart = "smoke-capture-start"
    /// A node row of the cluster upgrade plan.
    static let upgradePlan = "smoke-upgrade-plan"
    /// The list or first card of a feature screen, by its name in the walk.
    static func screen(_ name: String) -> String { "smoke-screen-\(name)" }
}

extension View {
    /// The smoke test's anchor when the element has one, nothing otherwise.
    @ViewBuilder
    func smokeID(_ id: String?) -> some View {
        if let id {
            accessibilityIdentifier(id)
        } else {
            self
        }
    }
}

/// The UI test's launch (`ICHOR_UI_TEST` in XCUIApplication.launchEnvironment): "demo" adds the
/// Talos demo cluster and shows it, "demo-kube" the Kubernetes one, through the same calls as
/// the Add a cluster screen's "Try demo". `ICHOR_UI_TEST_SCREEN` also opens one screen of it
/// (UITestScreen), for the screenshot walk. Unset, as in every real launch, nothing changes.
@MainActor
@Observable
final class UITestLaunch {
    static let shared = UITestLaunch()

    nonisolated private static let mode = ProcessInfo.processInfo.environment["ICHOR_UI_TEST"]
    nonisolated private static let screen = ProcessInfo.processInfo.environment["ICHOR_UI_TEST_SCREEN"]

    /// A UI test is driving the app: the demo tour's coach marks stay hidden (DemoTour.configure).
    nonisolated static var active: Bool { mode != nil }

    /// Why the demo could not be added; shown over the screens (RootView) for the test to report.
    private(set) var failure: String?
    /// The screen `ICHOR_UI_TEST_SCREEN` names, once the demo is on screen; MainNavigation opens it.
    private(set) var initialRoute: Route?
    private var prepared = false

    /// From the root view, which stays mounted: once the stored configs are loaded, the demo the
    /// test asked for, on screen. (The loading view's own task is cancelled as soon as it goes.)
    func prepare(_ model: AppModel) async {
        guard let mode = Self.mode, !prepared else { return }
        prepared = true
        let kube = mode == "demo-kube"
        guard kube || mode == "demo" else {
            failure = "Unknown ICHOR_UI_TEST value: \(mode)"
            return
        }
        while !model.loaded {
            try? await Task.sleep(for: .milliseconds(100))
        }
        do {
            if kube {
                try await model.saveKubeDemo()
            } else {
                try await model.save(yaml: try await TalosClient.demoConfig(), replacingSameCluster: true)
            }
        } catch {
            failure = "Demo not added: \(error.localizedDescription)"
            return
        }
        // The app data outlives a test: the other demo may already be there, and on screen.
        if let demo = model.summary?.contexts.first(where: { $0.demo && $0.isKube == kube }) {
            _ = model.selectCluster(fingerprint: demo.fingerprint)
        } else {
            failure = "Demo added but not found among the clusters"
            return
        }
        guard let screen = Self.screen else { return }
        do {
            initialRoute = try await UITestScreen.route(named: screen, model: model)
        } catch {
            failure = "Screen \(screen) not opened: \(error.localizedDescription)"
        }
    }
}

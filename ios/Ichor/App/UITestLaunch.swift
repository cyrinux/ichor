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
/// the Add a cluster screen's "Try demo". Unset, as in every real launch, nothing changes.
@MainActor
@Observable
final class UITestLaunch {
    static let shared = UITestLaunch()

    nonisolated private static let mode = ProcessInfo.processInfo.environment["ICHOR_UI_TEST"]

    /// A UI test is driving the app: the demo tour's coach marks stay hidden (DemoTour.configure).
    nonisolated static var active: Bool { mode != nil }

    /// Why the demo could not be added; shown over the screens (RootView) for the test to report.
    private(set) var failure: String?
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
        }
    }
}

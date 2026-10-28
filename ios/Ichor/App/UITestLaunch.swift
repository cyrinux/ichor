import Foundation

/// The anchors the demo-mode smoke test (IchorUITests/DemoSmokeTests.swift) waits for and taps,
/// the same names as Android's SmokeTags.kt. Accessibility identifiers are not shown or read out
/// (VoiceOver reads the labels); when a screen moves an anchor, move its identifier, not the test.
enum SmokeID {
    /// An overview node row.
    static let node = "smoke-node"
    /// The node hub's "Kernel log" entry.
    static let kernelLog = "smoke-node-kernel-log"
    /// The lines of a logs screen.
    static let logLines = "smoke-log-lines"
    /// The Kubernetes home of a cluster added from a kubeconfig.
    static let kubeHome = "smoke-kube-home"
}

/// The UI test's launch (`ICHOR_UI_TEST` in XCUIApplication.launchEnvironment): "demo" adds the
/// Talos demo cluster and shows it, "demo-kube" the Kubernetes one, through the same calls as
/// the Add a cluster screen's "Try demo". Unset, as in every real launch, nothing changes.
@MainActor
enum UITestLaunch {
    nonisolated private static let mode = ProcessInfo.processInfo.environment["ICHOR_UI_TEST"]

    /// A UI test is driving the app: the demo tour's coach marks stay hidden (DemoTour.configure).
    nonisolated static var active: Bool { mode != nil }

    /// Once the stored configs are loaded: the demo the test asked for, on screen.
    static func prepare(_ model: AppModel) async {
        guard let mode else { return }
        let kube = mode == "demo-kube"
        guard kube || mode == "demo" else { return }
        do {
            if kube {
                try await model.saveKubeDemo()
            } else {
                try await model.save(yaml: try await TalosClient.demoConfig(), replacingSameCluster: true)
            }
        } catch {
            // The test then times out on its first anchor, with the import screen in its tree.
            return
        }
        // The app data outlives a test: the other demo may already be there, and on screen.
        if let demo = model.summary?.contexts.first(where: { $0.demo && $0.isKube == kube }) {
            _ = model.selectCluster(fingerprint: demo.fingerprint)
        }
    }
}

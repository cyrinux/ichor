import Foundation
import IchorCore
import UIKit
import UserNotifications

/// The one node maintenance the app runs at a time (any node, any screen). Unlike an upgrade,
/// the app drives every step (cordon, evictions, reboot, uncordon): iOS suspending it stalls
/// the run, so a background task keeps it going a little while and a notification asks to
/// come back. A stop or a failure leaves the node cordoned.
@Observable
@MainActor
final class MaintenanceJob {
    static let shared = MaintenanceJob()

    struct Target: Equatable {
        let node: String
        let hostname: String
        let action: MaintenanceAction
        /// Cordoned before the run: a reboot leaves it cordoned.
        let wasCordoned: Bool
        /// A cluster without Talos: `node` is the Kubernetes node name, and the run a drain only.
        var kube = false
    }

    enum Outcome: Equatable {
        case succeeded
        case failed(String)
    }

    private(set) var target: Target?
    private(set) var events: [MaintenanceProgress] = []
    private(set) var outcome: Outcome?
    /// The user asked to stop: Go finishes the current step, then reports done.
    private(set) var stopping = false
    private var stop: (@Sendable () -> Void)?
    private var task: Task<Void, Never>?
    private var backgroundTask = UIBackgroundTaskIdentifier.invalid

    private static let foregroundNotification = "maintenance-keep-open"

    var isActive: Bool { task != nil }

    var timeline: [MaintenanceStep] {
        guard let action = target?.action else { return [] }
        switch outcome {
        case .succeeded?: return maintenanceTimeline(events, action: action, finished: true)
        case .failed(let message)?: return maintenanceTimeline(events, action: action, failure: message)
        case nil: return maintenanceTimeline(events, action: action)
        }
    }

    /// The drained pods with their latest state.
    var pods: [DrainPod] { latestDrainPods(events) }

    /// `acknowledged`: the user confirmed the plan's risks.
    func start(client: TalosClient, target: Target, includeBare: Bool, acknowledged: Bool) {
        guard !isActive else { return }
        self.target = target
        events = []
        outcome = nil
        stopping = false
        UIApplication.shared.isIdleTimerDisabled = true
        backgroundTask = UIApplication.shared.beginBackgroundTask(withName: "Node maintenance") {
            // Called on the main thread; the task must end before this returns.
            MainActor.assumeIsolated { self.endBackgroundTask() }
        }
        // For the reminder posted when the app leaves the screen.
        Task { _ = await BackgroundMonitor.requestPermission() }
        let run = target.kube
            ? client.startKubeDrain(kubeNode: target.node, includeBare: includeBare)
            : client.startMaintenance(node: target.node, action: target.action, includeBare: includeBare,
                                      acknowledged: acknowledged)
        stop = run.stop
        task = Task {
            var result = Outcome.failed(String(localized: "The maintenance ended without a result."))
            for await event in run.events {
                switch event {
                case .progress(let progress): events.append(progress)
                case .done(let error): result = error.map { .failed($0) } ?? .succeeded
                }
            }
            outcome = result
            task = nil
            stop = nil
            stopping = false
            endBackgroundTask()
            UNUserNotificationCenter.current().removeDeliveredNotifications(withIdentifiers: [Self.foregroundNotification])
            UIApplication.shared.isIdleTimerDisabled = UpgradeJob.shared.isActive
        }
    }

    /// The scene went to the background: reminds the user to come back while the run needs
    /// the app (no hostname: the lock screen may show it).
    func didEnterBackground() {
        guard isActive else { return }
        let content = UNMutableNotificationContent()
        content.title = String(localized: "Keep Ichor open")
        content.body = String(localized: "The node maintenance stops while Ichor is in the background. Open it until the maintenance ends.")
        content.sound = .default
        let request = UNNotificationRequest(identifier: Self.foregroundNotification, content: content, trigger: nil)
        Task { try? await UNUserNotificationCenter.current().add(request) }
    }

    /// Ends the background task once, whoever comes first (run over, or expiry).
    private func endBackgroundTask() {
        guard backgroundTask != .invalid else { return }
        UIApplication.shared.endBackgroundTask(backgroundTask)
        backgroundTask = .invalid
    }

    /// Stops after the current step; the node stays cordoned.
    func requestStop() {
        guard isActive, !stopping else { return }
        stopping = true
        stop?()
    }

    /// Forgets a finished run, so a new one can be planned.
    func clear() {
        guard !isActive else { return }
        target = nil
        events = []
        outcome = nil
    }
}

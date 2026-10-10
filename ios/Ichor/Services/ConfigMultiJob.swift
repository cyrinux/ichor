import Foundation
import IchorCore
import UIKit
import UserNotifications

/// The one multi-node config apply the app runs at a time. The app drives it node after node
/// (in reboot mode it waits for each one): iOS suspending it stalls the rollout, so, like a node
/// maintenance, a background task keeps it going a little while and a notification asks to come
/// back. The nodes done before a stop keep the change.
@Observable
@MainActor
final class ConfigMultiJob {
    static let shared = ConfigMultiJob()

    struct Target: Equatable {
        let nodes: [String]
        let mode: ConfigApplyMode
    }

    private(set) var target: Target?
    private(set) var progress: MultiConfigProgress?
    /// nil while running (or before); then nil inside on success, the error otherwise.
    private(set) var outcome: String??
    private var task: Task<Void, Never>?
    private var backgroundTask = UIBackgroundTaskIdentifier.invalid

    private static let foregroundNotification = "config-multi-keep-open"

    var isActive: Bool { task != nil }

    /// Starts the rollout unless one runs already; false then.
    @discardableResult
    func start(client: TalosClient, nodes: [String], edits: [ConfigEdit], mode: ConfigApplyMode) -> Bool {
        guard !isActive else { return false }
        target = Target(nodes: nodes, mode: mode)
        progress = nil
        outcome = nil
        UIApplication.shared.isIdleTimerDisabled = true
        backgroundTask = UIApplication.shared.beginBackgroundTask(withName: "Config rollout") {
            // Called on the main thread; the task must end before this returns.
            MainActor.assumeIsolated { self.endBackgroundTask() }
        }
        // For the reminder posted when the app leaves the screen.
        Task { _ = await BackgroundMonitor.requestPermission() }
        let events = client.applyMachineConfigMulti(nodes: nodes, edits: edits, mode: mode)
        task = Task {
            var result: String? = String(localized: "The config could not be applied.")
            for await event in events {
                switch event {
                case .progress(let update): progress = update
                case .done(let error): result = error
                }
            }
            outcome = .some(result)
            task = nil
            endBackgroundTask()
            UNUserNotificationCenter.current().removeDeliveredNotifications(withIdentifiers: [Self.foregroundNotification])
            UIApplication.shared.isIdleTimerDisabled = UpgradeJob.shared.isActive || MaintenanceJob.shared.isActive
            announce(result ?? String(localized: "Every node has the change."))
        }
        return true
    }

    /// The scene went to the background: reminds the user to come back while the rollout needs
    /// the app (no hostname: the lock screen may show it).
    func didEnterBackground() {
        guard isActive else { return }
        let content = UNMutableNotificationContent()
        content.title = String(localized: "Keep Ichor open")
        content.body = String(localized: "The config rollout stops while Ichor is in the background. Open it until every node is done.")
        content.sound = .default
        let request = UNNotificationRequest(identifier: Self.foregroundNotification, content: content, trigger: nil)
        Task { try? await UNUserNotificationCenter.current().add(request) }
    }

    /// Forgets a finished rollout, so a new one can start.
    func clear() {
        guard !isActive else { return }
        target = nil
        progress = nil
        outcome = nil
    }

    /// Ends the background task once, whoever comes first (rollout over, or expiry).
    private func endBackgroundTask() {
        guard backgroundTask != .invalid else { return }
        UIApplication.shared.endBackgroundTask(backgroundTask)
        backgroundTask = .invalid
    }
}

import Foundation
import IchorCore
import UIKit
import UserNotifications

/// The rolling cluster upgrade the app follows (one at a time). The app drives every step, so
/// iOS suspending it stalls the roll: a background task keeps it going a little while and a
/// notification asks to come back. A roll cut off holds its lock a few minutes, then starting
/// again from the plan continues with the nodes left.
@Observable
@MainActor
final class ClusterUpgradeJob {
    static let shared = ClusterUpgradeJob()

    private(set) var version: String?
    private(set) var progress: ClusterUpgradeProgress?
    /// nil while running (or before); then nil inside on success, the error otherwise.
    private(set) var outcome: String??
    /// Abort was asked: the roll ends before its next node.
    private(set) var aborting = false
    private var handle: ClusterUpgradeHandle?
    private var task: Task<Void, Never>?
    private var backgroundTask = UIBackgroundTaskIdentifier.invalid

    private static let foregroundNotification = "cluster-upgrade-keep-open"

    var isActive: Bool { task != nil }
    var paused: Bool { isActive && progress?.rollPhase == .paused }
    var nodes: [ClusterUpgradeNode] { progress?.nodes ?? [] }

    /// Starts the roll unless one runs already; false then.
    @discardableResult
    func start(client: TalosClient, version: String, drain: Bool, acknowledged: Bool) -> Bool {
        guard !isActive else { return false }
        self.version = version
        progress = nil
        outcome = nil
        aborting = false
        UIApplication.shared.isIdleTimerDisabled = true
        backgroundTask = UIApplication.shared.beginBackgroundTask(withName: "Cluster upgrade") {
            // Called on the main thread; the task must end before this returns.
            MainActor.assumeIsolated { self.endBackgroundTask() }
        }
        // For the reminder posted when the app leaves the screen.
        Task { _ = await BackgroundMonitor.requestPermission() }
        let run = client.startClusterUpgrade(version: version, drain: drain, acknowledged: acknowledged)
        handle = run
        task = Task {
            var result: String? = String(localized: "The upgrade ended without an answer: open the plan again to see where each node stands.")
            for await event in run.events {
                switch event {
                case .progress(let update): progress = update
                case .done(let error): result = error
                }
            }
            outcome = .some(result)
            task = nil
            handle = nil
            endBackgroundTask()
            UNUserNotificationCenter.current().removeDeliveredNotifications(withIdentifiers: [Self.foregroundNotification])
            UIApplication.shared.isIdleTimerDisabled = UpgradeJob.shared.isActive || MaintenanceJob.shared.isActive
            announce(result ?? String(localized: "Every node runs \(version)."))
        }
        return true
    }

    func pause() { if isActive { handle?.pause() } }

    func resume() { if isActive { handle?.resume() } }

    func abort() {
        guard isActive, !aborting else { return }
        aborting = true
        handle?.abort()
    }

    /// The scene went to the background: reminds the user to come back while the roll needs
    /// the app (no cluster name: the lock screen may show it).
    func didEnterBackground() {
        guard isActive else { return }
        let content = UNMutableNotificationContent()
        content.title = String(localized: "Keep Ichor open")
        content.body = String(localized: "The cluster upgrade stops while Ichor is in the background. Open it until every node is upgraded.")
        content.sound = .default
        let request = UNNotificationRequest(identifier: Self.foregroundNotification, content: content, trigger: nil)
        Task { try? await UNUserNotificationCenter.current().add(request) }
    }

    /// Forgets a finished roll, so the plan shows again.
    func clear() {
        guard !isActive else { return }
        version = nil
        progress = nil
        outcome = nil
        aborting = false
    }

    /// Ends the background task once, whoever comes first (roll over, or expiry).
    private func endBackgroundTask() {
        guard backgroundTask != .invalid else { return }
        UIApplication.shared.endBackgroundTask(backgroundTask)
        backgroundTask = .invalid
    }
}

import Foundation
import IchorCore
import UIKit
import UserNotifications

/// The Kubernetes upgrade (or its dry run) the app follows, one at a time. The app drives each
/// node in turn, so iOS suspending it stops the run: a background task keeps it going a little
/// while and a notification asks to come back. Running it again later continues, the
/// components already upgraded being left alone.
@Observable
@MainActor
final class K8sUpgradeJob {
    static let shared = K8sUpgradeJob()

    private(set) var version: String?
    private(set) var dryRun = false
    private(set) var events: [K8sUpgradeProgress] = []
    /// nil while running (or before); then nil inside on success, the error otherwise.
    private(set) var outcome: String??
    private(set) var cancelling = false
    private var handle: K8sUpgradeHandle?
    private var task: Task<Void, Never>?
    private var backgroundTask = UIBackgroundTaskIdentifier.invalid

    private static let foregroundNotification = "k8s-upgrade-keep-open"

    var isActive: Bool { task != nil }
    var latest: K8sUpgradeProgress? { events.last }

    @discardableResult
    func start(client: TalosClient, version: String, dryRun: Bool) -> Bool {
        guard !isActive else { return false }
        self.version = version
        self.dryRun = dryRun
        events = []
        outcome = nil
        cancelling = false
        UIApplication.shared.isIdleTimerDisabled = true
        backgroundTask = UIApplication.shared.beginBackgroundTask(withName: "Kubernetes upgrade") {
            // Called on the main thread; the task must end before this returns.
            MainActor.assumeIsolated { self.endBackgroundTask() }
        }
        Task { _ = await BackgroundMonitor.requestPermission() }
        let run = client.startK8sUpgrade(version: version, dryRun: dryRun)
        handle = run
        task = Task {
            var result: String? = String(localized: "The upgrade ended without an answer: open the plan again to see what changed.")
            for await event in run.events {
                switch event {
                case .progress(let update): events.append(update)
                case .done(let error): result = error
                }
            }
            outcome = .some(result)
            task = nil
            handle = nil
            endBackgroundTask()
            UNUserNotificationCenter.current().removeDeliveredNotifications(withIdentifiers: [Self.foregroundNotification])
            UIApplication.shared.isIdleTimerDisabled = UpgradeJob.shared.isActive || MaintenanceJob.shared.isActive
            announce(result ?? (dryRun ? String(localized: "Every node accepted its change. Nothing was applied.")
                                       : String(localized: "Every component runs \(version).")))
        }
        return true
    }

    /// Stops after the node being changed.
    func cancel() {
        guard isActive, !cancelling else { return }
        cancelling = true
        handle?.cancel()
    }

    func didEnterBackground() {
        guard isActive, !dryRun else { return }
        let content = UNMutableNotificationContent()
        content.title = String(localized: "Keep Ichor open")
        content.body = String(localized: "The Kubernetes upgrade stops while Ichor is in the background. Open it until every node is done.")
        content.sound = .default
        let request = UNNotificationRequest(identifier: Self.foregroundNotification, content: content, trigger: nil)
        Task { try? await UNUserNotificationCenter.current().add(request) }
    }

    func clear() {
        guard !isActive else { return }
        version = nil
        events = []
        outcome = nil
        cancelling = false
    }

    private func endBackgroundTask() {
        guard backgroundTask != .invalid else { return }
        UIApplication.shared.endBackgroundTask(backgroundTask)
        backgroundTask = .invalid
    }
}

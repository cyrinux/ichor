import Foundation
import IchorCore
import UIKit
import UserNotifications

/// The one node join the app runs at a time, like MaintenanceJob: the config goes to the node in
/// maintenance mode, then the app follows the install, the reboot and the join, and adds the node
/// to the context's nodes. iOS suspending the app stalls the following (not the install), so a
/// background task keeps it going a little while and a notification asks to come back.
@Observable
@MainActor
final class JoinJob {
    static let shared = JoinJob()

    enum Outcome: Equatable {
        /// The joined node's address.
        case joined(String)
        case failed(String)
    }

    private(set) var request: JoinRequest?
    private(set) var events: [JoinProgress] = []
    private(set) var outcome: Outcome?
    /// The user asked to stop following: Go reports done right after.
    private(set) var stopping = false
    private var stop: (@Sendable () -> Void)?
    private var task: Task<Void, Never>?
    private var backgroundTask = UIBackgroundTaskIdentifier.invalid

    private static let foregroundNotification = "join-keep-open"

    var isActive: Bool { task != nil }

    var timeline: [JoinStep] {
        switch outcome {
        case .joined?: joinTimeline(events, finished: true)
        case .failed(let message)?: joinTimeline(events, failure: message)
        case nil: joinTimeline(events)
        }
    }

    /// `register` adds the joined node's address to the stored talosconfig.
    func start(client: TalosClient, request: JoinRequest, register: @escaping @MainActor (String) async throws -> Void) {
        guard !isActive else { return }
        self.request = request
        events = []
        outcome = nil
        stopping = false
        UIApplication.shared.isIdleTimerDisabled = true
        backgroundTask = UIApplication.shared.beginBackgroundTask(withName: "Node join") {
            // Called on the main thread; the task must end before this returns.
            MainActor.assumeIsolated { self.endBackgroundTask() }
        }
        // For the reminder posted when the app leaves the screen.
        Task { _ = await BackgroundMonitor.requestPermission() }
        let run = client.startNodeJoin(request)
        stop = run.stop
        task = Task {
            var result = Outcome.failed(String(localized: "The join ended without a result."))
            for await event in run.events {
                switch event {
                case .progress(let progress): events.append(progress)
                case .done(let address, let error):
                    result = error.map { .failed($0) } ?? (address.isEmpty ? result : .joined(address))
                }
            }
            if case .joined(let address) = result {
                do {
                    try await register(address)
                } catch {
                    result = .failed(String(localized: "\(request.hostname) joined, but \(address) could not be added to the cluster's nodes: \(error.localizedDescription)"))
                }
            }
            outcome = result
            task = nil
            stop = nil
            stopping = false
            endBackgroundTask()
            UNUserNotificationCenter.current().removeDeliveredNotifications(withIdentifiers: [Self.foregroundNotification])
            UIApplication.shared.isIdleTimerDisabled = UpgradeJob.shared.isActive || MaintenanceJob.shared.isActive
        }
    }

    /// The scene went to the background: reminds the user to come back while the run needs the
    /// app (no hostname: the lock screen may show it).
    func didEnterBackground() {
        guard isActive else { return }
        let content = UNMutableNotificationContent()
        content.title = String(localized: "Keep Ichor open")
        content.body = String(localized: "Ichor stops following the new node while it is in the background. Open it until the node joins.")
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

    /// Stops following; once the config is sent the node goes on installing and joins by itself.
    func requestStop() {
        guard isActive, !stopping else { return }
        stopping = true
        stop?()
    }

    /// Forgets a finished run.
    func clear() {
        guard !isActive else { return }
        request = nil
        events = []
        outcome = nil
    }
}

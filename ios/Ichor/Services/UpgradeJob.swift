import Foundation
import IchorCore
import UIKit
import UserNotifications

/// The one Talos upgrade the app follows at a time (any node, any screen). Following stops
/// when the app quits or the user stops it; the node keeps upgrading either way. Until the
/// node reboots the app may drive the upgrade itself (Talos 1.18+ pulls and installs through
/// it), so a background task keeps it running a little while in the background.
@Observable
@MainActor
final class UpgradeJob {
    static let shared = UpgradeJob()

    struct Target: Equatable {
        let node: String
        let hostname: String
        let fromVersion: String
        let toVersion: String
        let image: String
        let stage: Bool
    }

    enum Outcome: Equatable {
        case succeeded(newVersion: String)
        case failed(String)
        /// The user stopped following; the result is unknown.
        case unfollowed
    }

    private(set) var target: Target?
    private(set) var events: [UpgradeProgress] = []
    private(set) var outcome: Outcome?
    private var task: Task<Void, Never>?
    private var backgroundTask = UIBackgroundTaskIdentifier.invalid
    /// Before the node reboots: suspending the app now can stall the upgrade.
    private(set) var needsForeground = false

    private static let foregroundNotification = "upgrade-keep-open"

    var isActive: Bool { task != nil }

    var timeline: [UpgradeStep] {
        switch outcome {
        case .succeeded?: upgradeTimeline(events, finished: true)
        case .failed(let message)?: upgradeTimeline(events, failure: message)
        case .unfollowed?, nil: upgradeTimeline(events)
        }
    }

    /// `acknowledged`: the user confirmed the plan's and the version's risks.
    func start(client: TalosClient, target: Target, force: Bool, acknowledged: Bool) {
        guard !isActive else { return }
        self.target = target
        events = [UpgradeProgress(phase: UpgradePhase.requested.rawValue, at: Date().epochMillis)]
        outcome = nil
        UIApplication.shared.isIdleTimerDisabled = true
        needsForeground = true
        backgroundTask = UIApplication.shared.beginBackgroundTask(withName: "Talos upgrade") {
            // Called on the main thread; the task must end before this returns.
            MainActor.assumeIsolated { self.endBackgroundTask() }
        }
        // For the reminder posted when the app leaves the screen too early.
        Task { _ = await BackgroundMonitor.requestPermission() }
        task = Task {
            var result = Outcome.unfollowed
            let stream = client.upgrade(node: target.node, image: target.image, stage: target.stage, force: force,
                                        acknowledged: acknowledged)
            for await event in stream {
                switch event {
                case .progress(let progress):
                    events.append(progress)
                    if let phase = UpgradePhase(go: progress.phase), !phase.needsApp { leaveRequestPhase() }
                case .done(let newVersion, let error):
                    result = error.map { .failed($0) } ?? .succeeded(newVersion: newVersion)
                }
            }
            outcome = result
            task = nil
            leaveRequestPhase()
            UIApplication.shared.isIdleTimerDisabled = MaintenanceJob.shared.isActive
        }
    }

    /// The scene went to the background: reminds the user to come back while the app still
    /// drives the upgrade (no hostname: the lock screen may show it).
    func didEnterBackground() {
        guard needsForeground else { return }
        let content = UNMutableNotificationContent()
        content.title = String(localized: "Keep Ichor open")
        content.body = String(localized: "The Talos upgrade can stall while Ichor is in the background. Open it until the node reboots.")
        content.sound = .default
        let request = UNNotificationRequest(identifier: Self.foregroundNotification, content: content, trigger: nil)
        Task { try? await UNUserNotificationCenter.current().add(request) }
    }

    /// The node reboots (or the run ended): the app is no longer needed on screen.
    private func leaveRequestPhase() {
        guard needsForeground else { return }
        needsForeground = false
        endBackgroundTask()
        UNUserNotificationCenter.current().removeDeliveredNotifications(withIdentifiers: [Self.foregroundNotification])
    }

    /// Ends the background task once, whoever comes first (request phase over, or expiry).
    private func endBackgroundTask() {
        guard backgroundTask != .invalid else { return }
        UIApplication.shared.endBackgroundTask(backgroundTask)
        backgroundTask = .invalid
    }

    /// Stops following (the upgrade itself cannot be cancelled once requested).
    func stopFollowing() { task?.cancel() }

    /// Forgets a finished upgrade, so a new one can be planned.
    func clear() {
        guard !isActive else { return }
        target = nil
        events = []
        outcome = nil
    }
}

/// The last Talos update check, kept in memory and repeated at most every 6 hours per set of
/// node versions; failures are silent (the banner just does not show).
@MainActor
enum TalosUpdateChecker {
    private static var info: TalosUpdateInfo?
    private static var versions: String?
    private static var lastCheck: Date?

    static func refresh(nodeVersions: [String]) async -> TalosUpdateInfo? {
        let csv = talosVersionsCSV(nodeVersions)
        guard !csv.isEmpty else { return nil }
        if csv == versions, !shouldCheckTalosUpdate(lastCheck: lastCheck) { return info }
        versions = csv
        lastCheck = Date()
        info = try? await TalosClient.talosUpdateCheck(versionsCSV: csv)
        return info
    }
}

import Foundation
import IchorCore
import UIKit

/// The one Talos upgrade the app follows at a time (any node, any screen). Following stops
/// when the app quits or the user stops it; the node keeps upgrading either way.
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

    var isActive: Bool { task != nil }

    var timeline: [UpgradeStep] {
        switch outcome {
        case .succeeded?: upgradeTimeline(events, finished: true)
        case .failed(let message)?: upgradeTimeline(events, failure: message)
        case .unfollowed?, nil: upgradeTimeline(events)
        }
    }

    func start(client: TalosClient, target: Target, force: Bool) {
        guard !isActive else { return }
        self.target = target
        events = [UpgradeProgress(phase: UpgradePhase.requested.rawValue, at: Int64(Date().timeIntervalSince1970 * 1000))]
        outcome = nil
        UIApplication.shared.isIdleTimerDisabled = true
        task = Task {
            var result = Outcome.unfollowed
            for await event in client.upgrade(node: target.node, image: target.image, stage: target.stage, force: force) {
                switch event {
                case .progress(let progress):
                    events.append(progress)
                case .done(let newVersion, let error):
                    result = error.map { .failed($0) } ?? .succeeded(newVersion: newVersion)
                }
            }
            outcome = result
            task = nil
            UIApplication.shared.isIdleTimerDisabled = false
        }
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

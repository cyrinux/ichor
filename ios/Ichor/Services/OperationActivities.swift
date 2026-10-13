import ActivityKit
import Foundation
import IchorCore
import Observation

/// The Live Activities of the runs the app follows (OperationKind), one per kind: started when
/// a run starts, updated on each step, ended with its result (gone 15 minutes later), dropped
/// when the run is cleared. They follow the jobs' state, so the jobs know nothing of them. An
/// activity is updated only while the app runs: once iOS suspends Ichor it shows the last step
/// with its time, and the jobs' "Keep Ichor open" notification still says why to come back.
/// With Live Activities turned off, nothing changes.
@MainActor
enum OperationActivities {
    /// What each kind's activity last showed: an update only when it changed.
    private static var shown: [OperationKind: OperationContent] = [:]
    private static var started = false

    /// At launch: the buttons' handler, the activities a previous launch left (their runs are
    /// gone with it), then following the jobs.
    static func start() {
        guard !started else { return }
        started = true
        OperationButtonHandler.perform = { kind, button in act(kind, button) }
        for activity in Activity<OperationAttributes>.activities {
            Task { await activity.end(nil, dismissalPolicy: .immediate) }
        }
        // The app lock turned on or off (a setting, not observable): mask or unmask at once.
        NotificationCenter.default.addObserver(forName: UserDefaults.didChangeNotification, object: nil, queue: .main) { _ in
            MainActor.assumeIsolated { resync() }
        }
        observe()
    }

    /// What the jobs show now, sent again only where it changed (the lock's masking).
    private static func resync() {
        let locked = lockEnabled
        guard locked != shownLocked else { return }
        sync(current())
    }

    /// The app lock as last shown.
    private static var shownLocked = false

    private static func observe() {
        let contents = withObservationTracking {
            current()
        } onChange: {
            Task { @MainActor in observe() }
        }
        sync(contents)
    }

    private static func sync(_ contents: [OperationKind: OperationContent]) {
        shownLocked = lockEnabled
        for kind in OperationKind.allCases {
            // Ended ones too: a finished run's card stays up to 15 minutes, until it is cleared
            // or the next run of its kind replaces it.
            let all = Activity<OperationAttributes>.activities.filter { $0.attributes.kind == kind }
            let activity = all.first { $0.activityState == .active }
            guard let content = contents[kind] else {
                shown[kind] = nil
                for gone in all { Task { await gone.end(nil, dismissalPolicy: .immediate) } }
                continue
            }
            if let last = shown[kind], !changed(last, content) { continue }
            shown[kind] = content
            let update = ActivityContent(state: content, staleDate: nil)
            if content.phase.ended {
                if let activity { Task { await activity.end(update, dismissalPolicy: .after(.now + 15 * 60)) } }
            } else if let activity {
                Task { await activity.update(update) }
            } else if ActivityAuthorizationInfo().areActivitiesEnabled {
                // The last run's result gives way to the new run.
                for gone in all { Task { await gone.end(nil, dismissalPolicy: .immediate) } }
                // Refused when the user turned them off meanwhile or too many run: the jobs'
                // notifications are the fallback, as before.
                _ = try? Activity.request(attributes: OperationAttributes(kind: kind), content: update)
            }
        }
    }

    /// The same content but for its time: not worth an update.
    private static func changed(_ old: OperationContent, _ new: OperationContent) -> Bool {
        var same = new
        same.updated = old.updated
        return same != old
    }

    /// A button on an activity (OperationIntent, in this process). Those that open the app when
    /// the app lock is on are links then; refused here too, the lock may have come on since.
    private static func act(_ kind: OperationKind, _ button: OperationButton) {
        if button.opensApp(for: kind, locked: lockEnabled) { return }
        switch (kind, button) {
        case (.upgrade, .stopFollowing): UpgradeJob.shared.stopFollowing()
        case (.join, .stopFollowing): JoinJob.shared.requestStop()
        case (.maintenance, .stop): MaintenanceJob.shared.requestStop()
        case (.configMulti, .stop): ConfigMultiJob.shared.stop()
        case (.k8sUpgrade, .stop): K8sUpgradeJob.shared.cancel()
        case (.imagePull, .stop): ImagePullJob.shared.stop()
        case (.imageScan, .stop): ImageScanJob.shared.stop()
        case (.clusterUpgrade, .pause): ClusterUpgradeJob.shared.pause()
        case (.clusterUpgrade, .resume): ClusterUpgradeJob.shared.resume()
        case (.clusterUpgrade, .abort): ClusterUpgradeJob.shared.abort()
        case (.configTry, .keep): ConfigTryJob.shared.keep()
        case (.configTry, .revert): ConfigTryJob.shared.revert()
        default: break
        }
    }

    // The setting AppModel keeps (Keys.lock).
    private static var lockEnabled: Bool { UserDefaults.standard.bool(forKey: "appLockEnabled") }

    /// What each run shows now; none for a kind with no run, nor one cleared.
    private static func current() -> [OperationKind: OperationContent] {
        let entries: [(OperationKind, OperationContent?)] = [
            (.upgrade, upgrade()), (.maintenance, maintenance()), (.configTry, configTry()), (.configMulti, configMulti()),
            (.clusterUpgrade, clusterUpgrade()), (.k8sUpgrade, k8sUpgrade()), (.imagePull, imagePull()), (.join, join()),
            (.imageScan, imageScan()),
        ]
        let locked = lockEnabled
        var contents: [OperationKind: OperationContent] = [:]
        for case let (kind, content?) in entries {
            contents[kind] = locked ? content.masked(title: kind.genericTitle) : content
        }
        return contents
    }

    private static func upgrade() -> OperationContent? {
        let job = UpgradeJob.shared
        guard let target = job.target else { return nil }
        let title = String(localized: "Upgrading \(target.hostname)…")
        switch job.outcome {
        case .succeeded(let version)?:
            return OperationContent(title: title, step: String(localized: "Upgraded to \(version)"), phase: .succeeded)
        case .failed(let message)?:
            return OperationContent(title: title, step: message, phase: .failed)
        case .unfollowed?:
            return OperationContent(title: title, step: String(localized: "Stopped following. The node keeps upgrading: check its version later."), phase: .stopped)
        case nil:
            let step = job.events.last.map(\.message).flatMap { $0.isEmpty ? nil : $0 } ?? String(localized: "Requested")
            return OperationContent(title: title, step: step, buttons: OperationButton.offered(for: .upgrade))
        }
    }

    private static func maintenance() -> OperationContent? {
        let job = MaintenanceJob.shared
        guard let target = job.target else { return nil }
        let title = String(localized: "Maintenance of \(target.hostname)")
        switch job.outcome {
        case .succeeded?: return OperationContent(title: title, step: String(localized: "Maintenance done"), phase: .succeeded)
        case .failed(let message)?: return OperationContent(title: title, step: message, phase: .failed)
        case nil:
            return OperationContent(title: title, step: job.events.last?.message ?? "",
                                    buttons: OperationButton.offered(for: .maintenance, stopping: job.stopping))
        }
    }

    private static func configTry() -> OperationContent? {
        let job = ConfigTryJob.shared
        guard let target = job.target else { return nil }
        let title = String(localized: "Trying a config change on \(target.hostname)")
        if let outcome = job.outcome {
            let phase: OperationPhase = switch outcome {
            case .kept: .succeeded
            case .reverted: .stopped
            case .failed: .failed
            }
            return OperationContent(title: title, step: ConfigTryJob.announcement(outcome), phase: phase)
        }
        let deadline = job.progress?.deadlineDate
        let step = deadline.map { String(localized: "Reverts by itself at \($0.formatted(date: .omitted, time: .shortened)) unless you keep it.") }
            ?? String(localized: "The node reverts by itself unless you keep the change.")
        return OperationContent(title: title, step: step, deadline: deadline, buttons: OperationButton.offered(for: .configTry, waiting: job.waiting))
    }

    private static func configMulti() -> OperationContent? {
        let job = ConfigMultiJob.shared
        guard job.target != nil else { return nil }
        let title = String(localized: "Config rollout")
        if let outcome = job.outcome {
            if job.stopped, let stopped = outcome { return OperationContent(title: title, step: stopped, phase: .stopped) }
            return OperationContent(title: title, step: outcome ?? String(localized: "Every node has the change."), phase: outcome == nil ? .succeeded : .failed)
        }
        guard let p = job.progress, let count = operationCount(index: p.index, total: p.total) else {
            return OperationContent(title: title, step: "", buttons: OperationButton.offered(for: .configMulti, stopping: job.stopped))
        }
        return OperationContent(title: title, step: String(localized: "\(count.done) of \(count.total) nodes done"), done: count.done, total: count.total,
                                buttons: OperationButton.offered(for: .configMulti, stopping: job.stopped))
    }

    private static func clusterUpgrade() -> OperationContent? {
        let job = ClusterUpgradeJob.shared
        guard let version = job.version else { return nil }
        let title = String(localized: "Upgrading the cluster to \(version)")
        if let outcome = job.outcome {
            return OperationContent(title: title, step: outcome ?? String(localized: "Every node runs \(version)."), phase: outcome == nil ? .succeeded : .failed)
        }
        let buttons = OperationButton.offered(for: .clusterUpgrade, stopping: job.aborting, paused: job.paused)
        guard let p = job.progress, let count = operationCount(index: p.index, total: p.total) else {
            return OperationContent(title: title, step: String(localized: "Starting the rollout…"), buttons: buttons)
        }
        let step = String(localized: "Upgrading \(min(p.index + 1, p.total))/\(p.total): \(p.name), \(p.localizedPhase)")
        return OperationContent(title: title, step: step, done: count.done, total: count.total, phase: job.paused ? .paused : .running, buttons: buttons)
    }

    private static func k8sUpgrade() -> OperationContent? {
        let job = K8sUpgradeJob.shared
        guard let version = job.version else { return nil }
        let title = job.dryRun ? String(localized: "Kubernetes dry run to \(version)") : String(localized: "Upgrading Kubernetes to \(version)")
        if let outcome = job.outcome {
            let done = job.dryRun ? String(localized: "Every node accepted its change. Nothing was applied.") : String(localized: "Every component runs \(version).")
            return OperationContent(title: title, step: outcome ?? done, phase: outcome == nil ? .succeeded : .failed)
        }
        let buttons = OperationButton.offered(for: .k8sUpgrade, stopping: job.cancelling)
        guard let p = job.latest else { return OperationContent(title: title, step: "", buttons: buttons) }
        let count = operationCount(index: p.index, total: p.total)
        return OperationContent(title: title, step: p.localizedStep, done: count?.done, total: count?.total, buttons: buttons)
    }

    private static func imagePull() -> OperationContent? {
        let job = ImagePullJob.shared
        guard job.hasRun else { return nil }
        let title = String(localized: "Pulling \(job.image)")
        let p = job.progress
        let step = String(localized: "\(p.done) of \(p.total) nodes done")
        let count = operationCount(index: p.done, total: p.total)
        if job.isRunning {
            return OperationContent(title: title, step: step, done: count?.done, total: count?.total,
                                    buttons: OperationButton.offered(for: .imagePull, stopping: job.outcome == .stopped))
        }
        switch job.outcome {
        case .succeeded?: return OperationContent(title: title, step: String(localized: "Image pulled"), done: count?.done, total: count?.total, phase: .succeeded)
        case .stopped?: return OperationContent(title: title, step: String(localized: "Image pull stopped"), phase: .stopped)
        case .failed(let message)?: return OperationContent(title: title, step: message, phase: .failed)
        case nil: return nil
        }
    }

    private static func join() -> OperationContent? {
        let job = JoinJob.shared
        guard let request = job.request else { return nil }
        let title = String(localized: "Adding \(request.hostname)")
        switch job.outcome {
        case .joined(let address)?:
            return OperationContent(title: title, step: String(localized: "\(request.hostname) joined the cluster as \(address)"), phase: .succeeded)
        case .failed(let message)?:
            return OperationContent(title: title, step: message, phase: .failed)
        case nil:
            return OperationContent(title: title, step: job.events.last?.message ?? "", buttons: OperationButton.offered(for: .join, stopping: job.stopping))
        }
    }

    private static func imageScan() -> OperationContent? {
        let job = ImageScanJob.shared
        guard let state = job.state else { return nil }
        let title = String(localized: "Image scan")
        if !state.running {
            if let error = state.error { return OperationContent(title: title, step: error, phase: .failed) }
            return state.stopped
                ? OperationContent(title: title, step: String(localized: "Scan stopped"), phase: .stopped)
                : OperationContent(title: title, step: String(localized: "Scan done"), phase: .succeeded)
        }
        let step = state.stopping ? String(localized: "Stopping…") : imageScanPhaseText(state.progress, scanner: state.scanner.label)
        let count = state.progress.flatMap { $0.phase == ImageScanPhase.scanning ? operationCount(index: $0.step - 1, total: $0.steps) : nil }
        return OperationContent(title: title, step: step, done: count?.done, total: count?.total,
                                buttons: OperationButton.offered(for: .imageScan, stopping: state.stopping))
    }
}

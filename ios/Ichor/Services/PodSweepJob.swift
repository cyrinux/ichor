import Foundation
import IchorCore

/// The "Clean up dead pods" sweep running or last run, app-wide and one at a time: Delete in
/// the sheet starts it and closes the sheet, its Live Activity counts "12 of 140 pods" with
/// Stop, and the Pods list reads it again once it ended. The deletes run in Go inside the app:
/// a background task keeps them going a little while once the app leaves the screen, past that
/// they wait until the app comes back.
@Observable
@MainActor
final class PodSweepJob {
    static let shared = PodSweepJob()

    private(set) var state: PodSweepState?
    private var stopSweep: (@Sendable () -> Void)?
    private let background = OperationBackgroundTask()

    var isRunning: Bool { state?.running == true }

    /// The sweep of `namespace` ("" every namespace) in `context`, nil when the last one was of
    /// another namespace or cluster.
    func sweep(context: String, namespace: String) -> PodSweepState? {
        guard let state, state.context == context, state.namespace == namespace else { return nil }
        return state
    }

    /// Starts deleting `names` (see `sanitizeNames`) of `namespace` in `context`; false while a
    /// sweep runs, or with nothing to delete.
    @discardableResult
    func start(client: TalosClient, context: String, namespace: String, names: String) -> Bool {
        let total = sanitizeCount(names)
        guard !isRunning, total > 0 else { return false }
        let sweep = client.startSanitize(namespace: namespace, names: names)
        // The handle to stop it is kept at once, and dropped once Go reported the sweep done.
        stopSweep = sweep.stop
        state = PodSweepState(context: context, namespace: namespace, total: total)
        OperationNotice.prepare()
        background.begin("Clean up dead pods")
        Task {
            for await event in sweep.events {
                switch event {
                case .progress(let progress):
                    state?.apply(progress)
                case .done(let result, let error):
                    state?.finish(result, error: error)
                }
            }
            stopSweep = nil
            background.end()
            if let state { OperationNotice.post(.podSweep, title: Self.title(state), body: state.error ?? Self.result(state)) }
        }
        return true
    }

    /// Ends the sweep after the pod being deleted; Go then reports it done.
    func stop() {
        guard isRunning else { return }
        state?.stopping = true
        stopSweep?()
    }
}

/// How the sweep is worded, in its Live Activity, its notification and the sheet.
extension PodSweepJob {
    static func title(_ state: PodSweepState) -> String {
        let place = state.namespace.isEmpty ? String(localized: "every namespace") : state.namespace
        if state.running { return String(localized: "Cleaning up dead pods · \(place)") }
        if state.error != nil { return String(localized: "Dead pod cleanup failed · \(place)") }
        return state.stopped ? String(localized: "Dead pod cleanup stopped · \(place)") : String(localized: "Dead pods cleaned up · \(place)")
    }

    /// "12 of 140 pods", or the stop on its way.
    static func step(_ state: PodSweepState) -> String {
        state.stopping ? String(localized: "Stopping after the current pod…") : progress(state)
    }

    static func progress(_ state: PodSweepState) -> String {
        let done = String(state.done), total = String(state.total)
        return String(localized: "\(done) of \(total) pods")
    }

    /// How many went, and how many could not be deleted when some could not.
    static func result(_ state: PodSweepState) -> String {
        let deleted = String(state.deleted)
        guard state.failedCount > 0 else { return String(localized: "Deleted: \(deleted)") }
        let failed = String(state.failedCount)
        return String(localized: "Deleted: \(deleted) · could not delete: \(failed)")
    }
}

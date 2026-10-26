import Foundation
import IchorCore

/// The cluster CA rotation running or last run, app-wide and one at a time, like PodSweepJob:
/// its Live Activity shows the step with Stop, and the rotation screen reads it. Each
/// talosconfig Go hands over is stored (in place of the context's credentials) before the run
/// is told to go on, so the phone never loses the cluster whatever step it stops at. The run
/// is in Go inside the app: a background task keeps it going a little while once the app
/// leaves the screen; past that it waits, and the cluster keeps the CAs it has at that step.
@Observable
@MainActor
final class CARotationJob {
    static let shared = CARotationJob()

    struct State: Equatable {
        let context: String
        var events: [CARotationProgress] = []
        var transitionStored = false
        var finalStored = false
        /// Why the final talosconfig could not be stored (the transition one keeps working).
        var finalStoreError: String?
        var finished = false
        var error: String?
        var stopping = false

        var running: Bool { !finished }
        var last: CARotationProgress? { events.last }
        var timeline: [CARotationStep] {
            caRotationTimeline(events, finished: finished && error == nil, failure: finished ? error : nil)
        }
    }

    private(set) var state: State?
    private var proceedRun: (@Sendable () -> Void)?
    private var stopRun: (@Sendable () -> Void)?
    private let background = OperationBackgroundTask()

    var isRunning: Bool { state?.running == true }

    /// Starts rotating `context`'s CA; false while one runs. `store` puts a talosconfig Go
    /// handed over in place of the context's credentials.
    @discardableResult
    func start(client: TalosClient, context: String, store: @escaping @MainActor (String) async throws -> Void) -> Bool {
        guard !isRunning else { return false }
        let run = client.startCARotation()
        proceedRun = run.proceed
        stopRun = run.stop
        state = State(context: context)
        OperationNotice.prepare()
        background.begin("Cluster CA rotation")
        Task {
            for await event in run.events {
                switch event {
                case .progress(let progress):
                    state?.events.append(progress)
                case .talosconfig(let yaml, let isFinal):
                    await stored(yaml, isFinal: isFinal, store: store)
                case .done(let error):
                    state?.finished = true
                    if state?.error == nil { state?.error = error }
                }
            }
            proceedRun = nil
            stopRun = nil
            background.end()
            if let state { OperationNotice.post(.caRotation, title: Self.title(state), body: state.error ?? Self.doneBody) }
        }
        return true
    }

    private func stored(_ yaml: String, isFinal: Bool, store: @MainActor (String) async throws -> Void) async {
        do {
            try await store(yaml)
            if isFinal {
                state?.finalStored = true
            } else {
                // The transition one is stored: the issuing CA may now change.
                state?.transitionStored = true
                proceedRun?()
            }
        } catch {
            if isFinal {
                state?.finalStoreError = error.localizedDescription
            } else {
                // Never go on without the phone's config: stop before the issuing CA changes.
                state?.error = error.localizedDescription
                state?.stopping = true
                stopRun?()
            }
        }
    }

    /// Stops before the next step; Go then reports the run done.
    func stop() {
        guard isRunning else { return }
        state?.stopping = true
        stopRun?()
    }

    /// Forgets a finished run.
    func dismiss() {
        if state?.finished == true { state = nil }
    }
}

/// How the rotation is worded, in its Live Activity, its notification and the screen.
extension CARotationJob {
    static func title(_ state: State) -> String {
        let context = state.context
        if state.running { return String(localized: "Rotating the CA of \(context)") }
        if state.error != nil { return String(localized: "CA rotation of \(context) stopped") }
        return String(localized: "CA of \(context) rotated")
    }

    static var doneBody: String {
        String(localized: "Every node now trusts the new CA only, and this phone’s talosconfig uses it. Issue new talosconfigs for your other devices and for talosctl.")
    }

    static func step(_ state: State) -> String {
        if state.stopping { return String(localized: "Stopping before the next step…") }
        return state.last?.message.nonEmpty ?? String(localized: "Check every node")
    }
}

extension OperationActivities {
    /// A CA rotation: its step of five and the last message, Stop; how it ended.
    static func caRotation() -> OperationContent? {
        guard let state = CARotationJob.shared.state else { return nil }
        let title = CARotationJob.title(state)
        if !state.running {
            if let error = state.error { return OperationContent(title: title, step: error, phase: .failed) }
            return OperationContent(title: title, step: CARotationJob.doneBody, phase: .succeeded)
        }
        let step = state.last.map { max($0.step - 1, 0) }
        return OperationContent(title: title, step: CARotationJob.step(state), done: step, total: state.last?.total,
                                buttons: OperationButton.offered(for: .caRotation, stopping: state.stopping))
    }
}

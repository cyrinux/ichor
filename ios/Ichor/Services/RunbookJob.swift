import Foundation
import IchorCore
import UIKit

/// The runbook running or last run, app-wide (one at a time): its screen reads it, and leaving
/// the screen keeps it going, with its Live Activity and Stop. A destructive step waits in
/// Go until its screen's confirmation: the activity says "Waiting for you", never a failure.
@Observable
@MainActor
final class RunbookJob {
    static let shared = RunbookJob()

    struct Run: Equatable {
        let id: String
        let fingerprint: String
        let plan: RunbookPlan
        var progress: RunbookProgress
        var running = true
        var stopping = false
        /// Why it did not start (the demo cluster, a runbook that no longer loads).
        var error: String?
        var result: RunbookResult?

        var stopped: Bool { !running && stopping }
        var succeeded: Bool { result?.ok == true }
    }

    private(set) var run: Run?
    private var handle: RunbookHandle?
    private let background = OperationBackgroundTask()

    var isRunning: Bool { run?.running == true }

    /// Starts runbook `id` of the cluster `fingerprint` unless one runs; false then.
    @discardableResult
    func start(id: String, fingerprint: String, yaml: String, plan: RunbookPlan, continueIDs: Set<String>, client: TalosClient) -> Bool {
        guard !isRunning else { return false }
        let started = client.startRunbook(yaml: yaml, continueIDs: continueIDs)
        handle = started
        run = Run(id: id, fingerprint: fingerprint, plan: plan, progress: RunbookProgress(plan: plan))
        OperationNotice.prepare()
        background.begin("Runbook")
        Task {
            for await event in started.events {
                switch event {
                case .step(let state): run?.progress.apply(state)
                case .done(let result):
                    run?.result = result
                    run?.progress.apply(result)
                case .failed(let message): run?.error = message
                }
            }
            finish()
        }
        return true
    }

    /// Lets the waiting destructive step `index` run.
    func confirm(_ index: Int) {
        guard isRunning, run?.progress.waiting?.index == index else { return }
        handle?.confirm(index)
    }

    /// Ends the run: a waiting step and the next ones are skipped.
    func stop() {
        guard isRunning else { return }
        run?.stopping = true
        handle?.stop()
    }

    /// Forgets the last run (its screen's Done).
    func reset() {
        if !isRunning { run = nil }
    }

    private func finish() {
        handle = nil
        run?.running = false
        background.end()
        guard let run, !run.stopped else { return }
        OperationNotice.post(.runbook, title: run.plan.name, body: Self.result(run))
    }
}

/// How a run is worded, on its screen, in its Live Activity and its notification.
extension RunbookJob {
    static func step(_ run: Run) -> String {
        if run.stopping { return String(localized: "Stopping…") }
        guard let current = run.progress.current, let position = run.progress.position else {
            return String(localized: "Starting…")
        }
        let count = String(localized: "Step \(String(position.step)) of \(String(position.total))")
        if current.state == .waiting { return "\(count) · \(String(localized: "Waiting for you"))" }
        return "\(count) · \(current.id)"
    }

    static func result(_ run: Run) -> String {
        if let error = run.error { return error }
        if run.stopped { return String(localized: "Runbook stopped.") }
        if run.succeeded { return String(localized: "Every step ran.") }
        let failed = run.progress.steps.first { $0.state == .failed }
        return failed.map { "\($0.id): \($0.message)" } ?? String(localized: "Runbook stopped.")
    }
}

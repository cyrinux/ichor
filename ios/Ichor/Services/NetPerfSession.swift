import Foundation
import IchorCore
import UIKit

/// The setup of a network test, the test running or last run, and the finished tests saved on
/// the phone, owned by the screen showing it (Kubernetes or Cluster insights): switching tabs
/// keeps a test running, leaving the screen stops it.
@Observable
@MainActor
final class NetPerfSession {
    /// A test started from the setup; `running` until Go reports it done.
    struct Run: Equatable {
        let setup: NetPerfSetup
        var progress: NetPerfProgress?
        var report: NetPerfReport?
        var running = true
        /// Stop was asked: Go is deleting the test namespace.
        var stopping = false
        var stopped = false
        var error: String?

        /// The measurements so far, in the order they ran.
        var results: [NetPerfResult] { report?.results ?? progress?.results ?? [] }
    }

    var setup = NetPerfSetup()
    private(set) var run: Run?
    private var stopTest: (@Sendable () -> Void)?

    /// Finished tests, newest first, under the scope `loadHistory` was given.
    private(set) var history: [NetPerfReport] = []
    /// A saved test shown instead of the setup.
    var viewing: NetPerfReport?
    private var store: InsightsStore?

    var isRunning: Bool { run?.running == true }

    /// Keeps the chosen nodes that are still there and ready, and picks a pair otherwise.
    func nodesLoaded(_ nodes: [NetPerfNode]) {
        setup = setup.withNodes(nodes)
    }

    func start(client: TalosClient) {
        guard !isRunning, setup.ready else { return }
        // Started here, before any async work: the handle to stop it is kept at once, and only
        // dropped once Go reported the test done (see finish).
        let test = client.netPerf(setup)
        stopTest = test.stop
        run = Run(setup: setup)
        UIApplication.shared.isIdleTimerDisabled = true
        Task {
            for await event in test.events {
                switch event {
                case .progress(let progress):
                    run?.progress = progress
                case .done(let report, let error):
                    let stopping = run?.stopping ?? false
                    run?.report = report
                    run?.stopped = stopping
                    // Stopping is not a failure, whatever Go reports for it.
                    run?.error = stopping ? nil : error
                    // A stopped or failed test is kept too, with what it measured.
                    if !report.results.isEmpty { keep(report) }
                }
            }
            finish()
        }
    }

    /// Ends the test early; Go deletes its namespace, then reports it done.
    func stop() {
        guard isRunning else { return }
        run?.stopping = true
        stopTest?()
    }

    /// Back to the setup, forgetting the last test.
    func reset() {
        if !isRunning { run = nil }
    }

    /// Reads the saved tests of `scope`: the cluster and the privacy mask, since Go masks node
    /// names in reports. Unreadable (e.g. its key gone), a new history starts over it.
    func loadHistory(scope: String) {
        let store = InsightsStore(scope: "netperf-\(scope)")
        guard store.scope != self.store?.scope else { return }
        self.store = store
        history = (try? store.read("history")).flatMap { try? TalosJSON.decode([NetPerfReport].self, from: $0) } ?? []
    }

    func delete(_ report: NetPerfReport) {
        history.removeAll { $0.started == report.started }
        viewing = nil
        save()
    }

    private func keep(_ report: NetPerfReport) {
        history = history.withReport(report)
        save()
    }

    /// A failed write keeps the history on screen until the screen is left.
    private func save() {
        guard let store else { return }
        if history.isEmpty {
            try? store.delete("history")
        } else if let data = try? JSONEncoder().encode(history) {
            try? store.save("history", json: String(decoding: data, as: UTF8.self))
        }
    }

    /// The screen went away: stops the test like Stop. The task keeps this session alive until
    /// Go deleted the test namespace and reported it done, then lets the screen sleep again.
    func leave() { stop() }

    private func finish() {
        stopTest = nil
        run?.running = false
        // A followed upgrade keeps the screen on too.
        UIApplication.shared.isIdleTimerDisabled = UpgradeJob.shared.isActive
    }
}

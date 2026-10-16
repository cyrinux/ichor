import Foundation
import IchorCore
import UIKit

/// The setup of a network test, the test running or last run, and the finished tests saved on
/// the phone, app-wide (one test at a time): the screens showing it (Kubernetes, KubeSpan,
/// Cluster insights) read it, and leaving them keeps the test running, with its Live Activity
/// and Stop. Go deletes the test namespace when the test ends and on Stop; the job
/// keeps the run's handle until Go reported that done.
@Observable
@MainActor
final class NetPerfJob {
    static let shared = NetPerfJob()

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
    private let background = OperationBackgroundTask()

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
        // The history of the cluster the test runs on, whichever is shown when it ends.
        let store = self.store
        UIApplication.shared.isIdleTimerDisabled = true
        OperationNotice.prepare()
        background.begin("Network test")
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
                    if !report.results.isEmpty { keep(report, in: store) }
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
        history = Self.read(store)
        viewing = nil
    }

    func delete(_ report: NetPerfReport) {
        history.removeAll { $0.started == report.started }
        viewing = nil
        Self.save(history, to: store)
    }

    /// Saves `report` in the history of `store`, the one shown or another cluster's.
    private func keep(_ report: NetPerfReport, in store: InsightsStore?) {
        guard let store else { return }
        if store.scope == self.store?.scope {
            history = history.withReport(report)
            Self.save(history, to: store)
        } else {
            Self.save(Self.read(store).withReport(report), to: store)
        }
    }

    private static func read(_ store: InsightsStore) -> [NetPerfReport] {
        (try? store.read("history")).flatMap { try? TalosJSON.decode([NetPerfReport].self, from: $0) } ?? []
    }

    /// A failed write keeps the history on screen until the app quits.
    private static func save(_ history: [NetPerfReport], to store: InsightsStore?) {
        guard let store else { return }
        if history.isEmpty {
            try? store.delete("history")
        } else if let data = try? JSONEncoder().encode(history) {
            try? store.save("history", json: String(decoding: data, as: UTF8.self))
        }
    }

    private func finish() {
        stopTest = nil
        run?.running = false
        background.end()
        // A followed upgrade or a node maintenance keeps the screen on too.
        UIApplication.shared.isIdleTimerDisabled = UpgradeJob.shared.isActive || MaintenanceJob.shared.isActive
        guard let run, !run.stopped else { return }
        OperationNotice.post(.netPerf, title: String(localized: "Network test"), body: Self.result(run))
    }
}

/// How the test is worded, on its screen, in its Live Activity and its notification.
extension NetPerfJob {
    /// What the test does now.
    static func step(_ run: Run) -> String {
        if run.stopping { return String(localized: "Stopping: deleting the test namespace…") }
        guard let p = run.progress else { return String(localized: "Preparing the test namespace…") }
        switch p.phase {
        case NetPerfPhase.starting:
            // "node: reason" while a pod waits, e.g. for its image.
            return p.message.contains(":") ? String(localized: "Waiting for \(p.message)…")
                : String(localized: "Starting netserver on \(run.setup.server)…")
        case NetPerfPhase.testing:
            return String(localized: "\(netPerfPathLabel(p.path)) · \(netPerfTestLabel(p.test)) (\(p.step)/\(p.steps))…")
        case NetPerfPhase.cleaning:
            return String(localized: "Deleting the test namespace…")
        default:
            return String(localized: "Preparing the test namespace…")
        }
    }

    static func result(_ run: Run) -> String {
        if run.stopped { return String(localized: "Test stopped.") }
        if let error = run.error { return String(localized: "The test failed: \(error)") }
        return String(localized: "Network test done")
    }
}

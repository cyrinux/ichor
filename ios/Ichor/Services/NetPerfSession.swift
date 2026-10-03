import Foundation
import IchorCore
import UIKit

/// The setup of a network test and the test running or last run, owned by the Kubernetes
/// screen: switching tabs keeps a test running, leaving the screen stops it.
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

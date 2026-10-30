import Foundation
import Ichorgo
import IchorCore

enum RunbookEvent: Sendable {
    case step(RunbookStepState)
    case done(RunbookResult)
    /// The runbook did not start (it does not load, the demo cluster).
    case failed(String)
}

/// A started runbook: its events (the stream finishes after done or failed), Stop, and the
/// confirmation of the destructive step waiting for it.
struct RunbookHandle: Sendable {
    let events: AsyncStream<RunbookEvent>
    let stop: @Sendable () -> Void
    let confirm: @Sendable (Int) -> Void
}

/// Runbooks (docs/runbooks.md): the cluster's list kept sealed by Go, the plan, and the run
/// (see TalosClient for the conventions). `scope` is RunbookStore's per-cluster key.
extension TalosClient {
    static func runbooks(scope: String) async throws -> [RunbookItem] {
        let list: RunbookList = try await json { IchorgoRunbookStoreList(scope, $0) }
        return list.items
    }

    /// Keeps `yaml` for the cluster, once its plan was shown; the same name replaces it.
    static func importRunbook(scope: String, yaml: String) async throws -> RunbookItem {
        try await json { IchorgoRunbookStoreImport(scope, yaml, $0) }
    }

    static func runbookYAML(scope: String, id: String) async throws -> String {
        try await run { IchorgoRunbookStoreGet(scope, id, $0) }
    }

    static func removeRunbook(scope: String, id: String) async throws {
        try await run { error -> Void in _ = IchorgoRunbookStoreRemove(scope, id, error) }
    }

    static func runbookSummary(yaml: String) async throws -> RunbookSummary {
        try await json { IchorgoRunbookSummary(yaml, $0) }
    }

    /// A runbook file from an https URL (as a plugin's: https only, 1 MiB, 20 s).
    static func downloadRunbook(url: String) async throws -> String {
        try await run { IchorgoPluginFetchURL(url, $0) }
    }

    /// What each step will do, its role and whether the cluster allows it; runs nothing.
    func runbookPlan(yaml: String) async throws -> RunbookPlan {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoRunbookPlan(config, context, kubeServer, yaml, $0)
        }
    }

    /// Starts the run; `continueIDs` are the steps chosen to go past. Each destructive step
    /// waits until `confirm(index)`; `stop` skips the rest.
    func startRunbook(yaml: String, continueIDs: Set<String>) -> RunbookHandle {
        let (stream, continuation) = AsyncStream.makeStream(of: RunbookEvent.self)
        let bridge = RunbookBridge(continuation)
        let run = IchorgoStartRunbook(
            kubeConfig, kubeContext, kubeAPIServer, yaml, runbookContinueIDs(continueIDs), bridge).map(GoObject.init)
        continuation.onTermination = { _ in
            _ = bridge  // keep the listener alive for the whole run
        }
        return RunbookHandle(events: stream, stop: { run?.value.stop() }, confirm: { run?.value.confirm($0) })
    }
}

private final class RunbookBridge: NSObject, IchorgoRunbookListenerProtocol, Sendable {
    private let continuation: AsyncStream<RunbookEvent>.Continuation

    init(_ continuation: AsyncStream<RunbookEvent>.Continuation) {
        self.continuation = continuation
    }

    func onStep(_ index: Int, stateJSON: String?) {
        guard let stateJSON, let state = try? TalosJSON.decode(RunbookStepState.self, from: stateJSON) else { return }
        continuation.yield(.step(state))
    }

    func onDone(_ resultJSON: String?) {
        let result =
            resultJSON.flatMap { try? TalosJSON.decode(RunbookResult.self, from: $0) }
            ?? RunbookResult(ok: false, steps: [])
        continuation.yield(.done(result))
        continuation.finish()
    }

    func onError(_ message: String?) {
        continuation.yield(.failed(message ?? ""))
        continuation.finish()
    }
}

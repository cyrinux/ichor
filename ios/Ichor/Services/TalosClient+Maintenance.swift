import Foundation
import Ichorgo
import IchorCore

enum MaintenanceEvent: Sendable {
    case progress(MaintenanceProgress)
    /// nil on success.
    case done(error: String?)
}

/// Node maintenance calls: cordon, drain plan and the cordon → drain → reboot run (see
/// TalosClient for the conventions).
extension TalosClient {
    /// The pods a drain evicts or leaves, their budgets, and the checks of a reboot (os:admin).
    func maintenancePlan(node: String) async throws -> MaintenancePlan {
        try await Self.json { [config, context, kubeServer] in IchorgoNodeMaintenancePlan(config, context, kubeServer, node, $0) }
    }

    /// `kubectl cordon` (on) or `uncordon` the node (os:admin).
    func cordon(node: String, on: Bool) async throws {
        try await Self.run { [config, context, kubeServer] error -> Void in
            _ = IchorgoKubeCordon(config, context, kubeServer, node, on, error)
        }
    }

    /// Cordons, drains, then reboots, shuts down or stops (`action`). After a stop Go still
    /// reports `done` (the node stays cordoned), so stop rather than cancel the consuming task.
    /// `acknowledged`: the user confirmed the plan's risks.
    func startMaintenance(node: String, action: MaintenanceAction, includeBare: Bool,
                          acknowledged: Bool) -> (events: AsyncStream<MaintenanceEvent>, stop: @Sendable () -> Void) {
        Self.followMaintenance { [config, context, kubeServer] bridge in
            IchorgoStartNodeMaintenance(config, context, kubeServer, node, action.rawValue, includeBare, acknowledged, bridge)
        }
    }

    /// A cluster without Talos (kubeconfig): the drain plan of the Kubernetes node `kubeNode`,
    /// without the reboot checks.
    func kubeDrainPlan(kubeNode: String) async throws -> MaintenancePlan {
        try await Self.json { [config = kubeConfig, context = kubeContext, kubeServer = kubeAPIServer] in
            IchorgoKubeDrainPlan(config, context, kubeServer, kubeNode, $0)
        }
    }

    /// A cluster without Talos: `kubectl cordon` (on) or `uncordon` of the Kubernetes node `kubeNode`.
    func kubeNodeCordon(kubeNode: String, on: Bool) async throws {
        try await Self.run { [config = kubeConfig, context = kubeContext, kubeServer = kubeAPIServer] error -> Void in
            _ = IchorgoKubeNodeCordon(config, context, kubeServer, kubeNode, on, error)
        }
    }

    /// A cluster without Talos: cordons and drains the Kubernetes node `kubeNode`, which stays
    /// cordoned. Reports like startMaintenance (phases cordon and drain).
    func startKubeDrain(kubeNode: String, includeBare: Bool) -> (events: AsyncStream<MaintenanceEvent>, stop: @Sendable () -> Void) {
        Self.followMaintenance { [config = kubeConfig, context = kubeContext, kubeServer = kubeAPIServer] bridge in
            IchorgoStartKubeDrain(config, context, kubeServer, kubeNode, includeBare, bridge)
        }
    }

    /// The events of the run `start` begins with a listener.
    private static func followMaintenance(
        _ start: (MaintenanceBridge) -> IchorgoMaintenanceRun?
    ) -> (events: AsyncStream<MaintenanceEvent>, stop: @Sendable () -> Void) {
        let (stream, continuation) = AsyncStream.makeStream(of: MaintenanceEvent.self)
        let bridge = MaintenanceBridge(
            progress: { continuation.yield(.progress($0)) },
            done: {
                continuation.yield(.done(error: $0))
                continuation.finish()
            }
        )
        let run = start(bridge)
        continuation.onTermination = { _ in
            run?.cancel()
            _ = bridge // keep the listener alive for the whole run
        }
        return (stream, { run?.cancel() })
    }
}

private final class MaintenanceBridge: NSObject, IchorgoMaintenanceListenerProtocol, @unchecked Sendable {
    private let sink: JSONSink<MaintenanceProgress>

    init(progress: @escaping @Sendable (MaintenanceProgress) -> Void, done: @escaping @Sendable (String?) -> Void) {
        sink = JSONSink(item: progress, done: done)
    }

    func onProgress(_ json: String?) { sink.emit(json) }

    func onDone(_ errMessage: String?) { sink.finish(errMessage) }
}

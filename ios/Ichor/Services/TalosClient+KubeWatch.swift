import Foundation
import Ichorgo
import IchorCore

/// What a list kept live reports: its changes, then how it ended (`error` nil when cancelled).
enum KubeWatchStreamEvent<T: Sendable>: Sendable {
    case change(KubeWatchEvent<T>)
    case done(error: String?)
}

/// What a view kept live reports: each update, then how it ended (`error` nil when cancelled).
enum KubeLiveEvent<T: Sendable>: Sendable {
    case update(T)
    case done(error: String?)
}

/// Lists and views the Go core keeps live through the Kubernetes API's watches (kube_watch.go,
/// os:admin): followed while a screen is shown and the app active, cancelled otherwise.
/// Pull-to-refresh stays: a watch adds to the one-shot reads, it does not replace them.
extension TalosClient {
    /// The pods a workload's selector matches, narrowed to `phase`, kept live: the whole list
    /// first (full objects), then each pod added, changed or gone.
    func workloadPodsWatch(kind: String, namespace: String, name: String, phase: PodPhaseFilter) -> AsyncStream<KubeWatchStreamEvent<KubePod>> {
        let query = phase.query
        return podsWatch { bridge in
            IchorgoStartKubeWorkloadPodsWatch(kubeConfig, kubeContext, kubeAPIServer, kind, namespace, name, query, bridge)
        }
    }

    /// The pods of the Kubernetes node `kubeNode` (every namespace), narrowed to `phase`, kept
    /// live like `workloadPodsWatch`.
    func nodePodsWatch(kubeNode: String, phase: PodPhaseFilter) -> AsyncStream<KubeWatchStreamEvent<KubePod>> {
        let query = phase.query
        return podsWatch { bridge in
            IchorgoStartKubeNodePodsWatch(kubeConfig, kubeContext, kubeAPIServer, kubeNode, query, bridge)
        }
    }

    /// A pod watch `start` opens: SYNC pages and single pods decoded, the end reported.
    private func podsWatch(_ start: @escaping (KubeWatchBridge) -> IchorgoKubeWatchRun?) -> AsyncStream<KubeWatchStreamEvent<KubePod>> {
        TalosClient.bridged { continuation in
            let bridge = KubeWatchBridge(
                event: { eventType, json in
                    let list: (String) throws -> [KubePod] = { try TalosJSON.decode(KubePodPage.self, from: $0).pods }
                    if let change = KubeWatchEvent<KubePod>.decode(eventType, json: json, list: list) {
                        continuation.yield(.change(change))
                    }
                },
                done: {
                    continuation.yield(.done(error: $0))
                    continuation.finish()
                }
            )
            let run = start(bridge)
            return BridgedRun(bridge) { run?.cancel() }
        }
    }

    /// `rolloutStatus` kept live: the status at the start, then again each time the workload or
    /// one of its pods changes.
    func rolloutWatch(_ workload: KubeWorkload) -> AsyncStream<KubeLiveEvent<KubeRolloutStatus>> {
        live { bridge in
            IchorgoStartKubeRolloutWatch(kubeConfig, kubeContext, kubeAPIServer, workload.kind, workload.namespace, workload.name, bridge)
        }
    }

    /// `objectSummary` kept live: the summary at the start, then again each time the object or
    /// its events change.
    func objectSummaryWatch(_ resource: KubeAPIResource, namespace: String, name: String) -> AsyncStream<KubeLiveEvent<KubeObjectSummary>> {
        live { bridge in
            IchorgoStartKubeObjectWatch(kubeConfig, kubeContext, kubeAPIServer, resource.group, resource.version, resource.resource,
                                        namespace, name, bridge)
        }
    }

    /// A signal each time one of `kinds` changes in `namespace` (nil: every namespace), at most
    /// every 2 s and never for the lists read at the start: for the lists that cannot be merged
    /// row by row, read again on each signal.
    func changeWatch(namespace: String?, kinds: [String]) -> AsyncStream<KubeLiveEvent<KubeChange>> {
        let joined = kinds.joined(separator: ",")
        return live { bridge in
            IchorgoStartKubeChangeWatch(kubeConfig, kubeContext, kubeAPIServer, namespace ?? "", joined, bridge)
        }
    }

    /// A view kept live by the Go run `start` makes for the bridge: each update decoded as `T`,
    /// only the latest one kept for a slow consumer.
    private func live<T: Decodable & Sendable>(_ start: (KubeLiveBridge) -> IchorgoKubeLiveRun?) -> AsyncStream<KubeLiveEvent<T>> {
        TalosClient.bridged(buffering: .bufferingNewest(1)) { continuation in
            let bridge = KubeLiveBridge(
                update: { json in
                    if let value = try? TalosJSON.decode(T.self, from: json) { continuation.yield(.update(value)) }
                },
                done: {
                    continuation.yield(.done(error: $0))
                    continuation.finish()
                }
            )
            let run = start(bridge)
            return BridgedRun(bridge) { run?.cancel() }
        }
    }
}

private final class KubeWatchBridge: NSObject, IchorgoKubeWatchListenerProtocol, @unchecked Sendable {
    private let event: @Sendable (String, String) -> Void
    private let done: @Sendable (String?) -> Void

    init(event: @escaping @Sendable (String, String) -> Void, done: @escaping @Sendable (String?) -> Void) {
        self.event = event
        self.done = done
    }

    func onEvent(_ eventType: String?, json: String?) {
        event(eventType ?? "", json ?? "")
    }

    func onDone(_ errMessage: String?) {
        done(errMessage.nonEmpty)
    }
}

private final class KubeLiveBridge: NSObject, IchorgoKubeLiveListenerProtocol, @unchecked Sendable {
    private let update: @Sendable (String) -> Void
    private let done: @Sendable (String?) -> Void

    init(update: @escaping @Sendable (String) -> Void, done: @escaping @Sendable (String?) -> Void) {
        self.update = update
        self.done = done
    }

    func onUpdate(_ json: String?) {
        update(json ?? "")
    }

    func onDone(_ errMessage: String?) {
        done(errMessage.nonEmpty)
    }
}

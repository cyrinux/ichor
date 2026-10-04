import Foundation
import IchorCore
import Ichorgo

enum HubbleEvent: Sendable {
    case update(HubbleSnapshot)
    /// `error` is nil when cancelled.
    case done(error: String?)
}

/// Network policies (any CNI) and Cilium's live flows, through the Kubernetes API (os:admin).
extension TalosClient {
    /// Whether Cilium runs, with Hubble, and its agents.
    func cilium() async throws -> CiliumStatus {
        try await Self.json { [config, context, kubeServer] in IchorgoKubeCilium(config, context, kubeServer, $0) }
    }

    /// Every NetworkPolicy, CiliumNetworkPolicy and CiliumClusterwideNetworkPolicy, with the pods
    /// each selects and how isolated each namespace is.
    func networkPolicies() async throws -> NetPolicyReport {
        try await Self.json { [config, context, kubeServer] in IchorgoKubeNetworkPolicies(config, context, kubeServer, $0) }
    }

    /// Follows the cluster's flows live (`hubble observe --follow` in every cilium-agent);
    /// cancelling the consuming task stops it.
    func hubbleFlows(_ filter: HubbleFilter) -> AsyncStream<HubbleEvent> {
        AsyncStream(bufferingPolicy: .bufferingNewest(4)) { continuation in
            let bridge = HubbleBridge(
                update: { continuation.yield(.update($0)) },
                done: {
                    continuation.yield(.done(error: $0))
                    continuation.finish()
                }
            )
            let wire = filter.wire
            let run = IchorgoStartHubbleFlows(config, context, kubeServer, wire.namespace, wire.pod, filter.dropsOnly, bridge)
            continuation.onTermination = { _ in
                run?.cancel()
                _ = bridge // keep the listener alive for the whole stream
            }
        }
    }
}

private final class HubbleBridge: NSObject, IchorgoHubbleListenerProtocol, @unchecked Sendable {
    private let update: @Sendable (HubbleSnapshot) -> Void
    private let done: @Sendable (String?) -> Void

    init(update: @escaping @Sendable (HubbleSnapshot) -> Void, done: @escaping @Sendable (String?) -> Void) {
        self.update = update
        self.done = done
    }

    func onUpdate(_ json: String?) {
        guard let json, let decoded = try? TalosJSON.decode(HubbleSnapshot.self, from: json) else { return }
        update(decoded)
    }

    func onDone(_ errMessage: String?) {
        done(errMessage.nonEmpty)
    }
}

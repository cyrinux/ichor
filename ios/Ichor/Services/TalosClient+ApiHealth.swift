import Foundation
import IchorCore
import Ichorgo

/// The Kubernetes API server's own health and load, through the Kubernetes API (os:admin).
extension TalosClient {
    /// readyz and livez, then two /metrics scrapes a few seconds apart for the live rates: who
    /// sends requests, the priority levels, the busiest requests and what waits in the queues.
    func apiHealth() async throws -> ApiHealthReport {
        try await Self.json { [config, context, kubeServer] in IchorgoKubeAPIHealth(config, context, kubeServer, $0) }
    }
}

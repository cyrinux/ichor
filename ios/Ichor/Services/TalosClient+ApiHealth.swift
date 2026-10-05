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

    /// Who loads the API server over the last `minutes`, from the control planes' audit logs
    /// read through the Talos API (os:admin, tens of MB).
    func auditAnalysis(minutes: Int) async throws -> AuditReport {
        try await Self.json { [config, context] in IchorgoKubeAuditAnalysis(config, context, minutes, $0) }
    }
}

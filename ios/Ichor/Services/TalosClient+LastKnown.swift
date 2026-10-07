import Foundation
import Ichorgo
import IchorCore

extension TalosClient {
    /// A screen's data as decoded, with the raw JSON it came from (kept by LastKnownStore).
    /// Decoded here, off the main actor.
    func fetch<T: Decodable & Sendable>(_ domain: LastKnownDomain, as type: T.Type = T.self) async throws -> (value: T, json: String) {
        let json = try await rawJSON(domain)
        return (try TalosJSON.decode(T.self, from: json), json)
    }

    /// The Go call behind each domain, as the plain methods make it.
    private func rawJSON(_ domain: LastKnownDomain) async throws -> String {
        // Kubernetes domains through the cluster's Kubernetes access (kubeConfig), Talos ones as is.
        try await Self.run { [config, context, kubeConfig = self.kubeConfig, kubeContext = self.kubeContext, kubeServer = self.kubeAPIServer] error -> String in
            switch domain {
            case .overview: return IchorgoClusterOverview(config, context, error)
            case .etcd: return IchorgoEtcdStatus(config, context, error)
            case .kubespan: return IchorgoKubeSpanStatus(config, context, error)
            case .inventory: return IchorgoClusterInventory(config, context, error)
            case .workloads: return IchorgoKubeWorkloads(kubeConfig, kubeContext, kubeServer, error)
            case .pods: return IchorgoKubePods(kubeConfig, kubeContext, kubeServer, error)
            case .cronJobs: return IchorgoKubeCronJobs(kubeConfig, kubeContext, kubeServer, error)
            case .services(let node): return IchorgoNodeServices(config, context, node, error)
            case .resources(let node): return IchorgoNodeResources(config, context, node, error)
            case .hardware(let node): return IchorgoNodeHardware(config, context, node, error)
            case .network(let node): return IchorgoNodeNetwork(config, context, node, error)
            case .images(let node): return IchorgoNodeImages(config, context, node, error)
            }
        }
    }
}

import Foundation
import Talosmobile
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
        try await Self.run { [config, context] error -> String in
            switch domain {
            case .overview: return TalosmobileClusterOverview(config, context, error)
            case .etcd: return TalosmobileEtcdStatus(config, context, error)
            case .kubespan: return TalosmobileKubeSpanStatus(config, context, error)
            case .inventory: return TalosmobileClusterInventory(config, context, error)
            case .workloads: return TalosmobileKubeWorkloads(config, context, error)
            case .pods: return TalosmobileKubePods(config, context, error)
            case .services(let node): return TalosmobileNodeServices(config, context, node, error)
            case .resources(let node): return TalosmobileNodeResources(config, context, node, error)
            case .hardware(let node): return TalosmobileNodeHardware(config, context, node, error)
            case .network(let node): return TalosmobileNodeNetwork(config, context, node, error)
            case .images(let node): return TalosmobileNodeImages(config, context, node, error)
            }
        }
    }
}

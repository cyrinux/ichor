import Foundation
import IchorCore

/// The cluster maps (Go ClusterTopology) that order the overview's and the Nodes screen's
/// nodes site by site. Android only uses a map the KubeSpan screen fetched; here the overview
/// asks for it itself, in the background, once per cluster: the call reads four small
/// resources per node (in parallel), cheap next to the overview, but not worth repeating on
/// every refresh since sites rarely change. A pull to refresh asks again. Kept in memory only,
/// keyed on the context and the screenshot mode generation, so another cluster's (or
/// unmasked) sites never show.
@Observable
@MainActor
final class TopologyStore {
    static let shared = TopologyStore()

    private var maps: [String: ClusterTopology] = [:]
    @ObservationIgnored private var loading: Set<String> = []

    /// The map loaded for key, nil when none yet.
    func topology(for key: String) -> ClusterTopology? { maps[key] }

    /// Loads the map for key unless it is already there (or `force`). Best effort: a failure
    /// keeps what was there, and the nodes stay in one group without a map.
    func load(with client: TalosClient, key: String, force: Bool = false) async {
        if !force, maps[key] != nil { return }
        guard !loading.contains(key) else { return }
        loading.insert(key)
        defer { loading.remove(key) }
        guard let loaded = try? await client.topology() else { return }
        maps[key] = loaded
    }

    /// A map read elsewhere (the KubeSpan screen's map tab), kept so the overview groups its
    /// nodes by the latest sites without asking again.
    func remember(_ topology: ClusterTopology, for key: String) {
        maps[key] = topology
    }
}

extension AppModel {
    /// What a cluster map belongs to: the context and the screenshot mode generation.
    var topologyKey: String { "\(activeContext)#\(dataGeneration)" }
}

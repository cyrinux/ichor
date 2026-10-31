import Foundation
import IchorCore

/// The cluster maps (Go ClusterTopology) that order the overview's and the Nodes screen's
/// nodes site by site and tell the home summary when a KubeSpan tunnel is down. As on Android,
/// the overview asks for one itself, in the background, after each load: a tunnel can die
/// between two loads, and the call only reads four small resources per node (in parallel),
/// cheap next to the overview. A map under 30 s old is not read again (see topologyIsFresh);
/// a pull to refresh always is. Kept in memory only, keyed on the context and the screenshot
/// mode generation, so another cluster's (or
/// unmasked) sites never show. Each new map keeps what the one before knew of the nodes that
/// no longer answer (mergeLastKnown): their name and site.
@Observable
@MainActor
final class TopologyStore {
    static let shared = TopologyStore()

    private var maps: [String: (topology: ClusterTopology, at: Date)] = [:]
    @ObservationIgnored private var loading: Set<String> = []

    /// The map loaded for key, nil when none yet.
    func topology(for key: String) -> ClusterTopology? { maps[key]?.topology }

    /// Loads the map for key unless one was just read (or `force`). Best effort: a failure
    /// keeps what was there, and the nodes stay in one group without a map.
    func load(with client: TalosClient, key: String, force: Bool = false) async {
        if !force, let held = maps[key], topologyIsFresh(readAt: held.at) { return }
        guard !loading.contains(key) else { return }
        loading.insert(key)
        defer { loading.remove(key) }
        guard let loaded = try? await client.topology() else { return }
        remember(loaded, for: key)
    }

    /// A map read elsewhere (the KubeSpan screen's map tab), kept so the overview groups its
    /// nodes by the latest sites without asking again. Returns it as kept: with the nodes that
    /// no longer answer filled in from the map before.
    @discardableResult
    func remember(_ topology: ClusterTopology, for key: String) -> ClusterTopology {
        let previous = maps[key]
        let merged = mergeLastKnown(current: topology, previous: previous?.topology, previousAt: previous?.at ?? Date())
        maps[key] = (merged, Date())
        return merged
    }
}

extension AppModel {
    /// What a cluster map belongs to: the context and the screenshot mode generation.
    var topologyKey: String { "\(activeContext)#\(dataGeneration)" }
}

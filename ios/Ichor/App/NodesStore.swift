import Foundation
import Observation
import IchorCore

/// What each node of the cluster on screen can do and whether it answers, which nodes report
/// throttled sensors, and the discovered members set aside. All of it resets on a cluster switch,
/// except the members set aside, which are kept per context until the app restarts.
@Observable
@MainActor
final class NodesStore {
    /// What this store has to reach: the features call, the last known overview and Wake-on-LAN.
    /// Set once by AppModel.init; weak, the model owns this store.
    @ObservationIgnored weak var model: AppModel?

    /// What each node's Talos version can do (Go NodeFeatures), by node address; loaded once
    /// per node and Talos version, see loadFeatures.
    private(set) var nodeFeatures: [String: NodeFeatures] = [:]

    /// The nodes found throttled when their Hardware screen last read their sensors (by
    /// address): the overview never reads sensors itself, so a row says nothing until then.
    private(set) var throttledNodes: Set<String> = []

    /// Whether each node of the last overview answered (by address), and the nodes whose
    /// features call just failed to dial them (gone down since): see reachability(of:).
    private(set) var nodeReachability: [String: NodeReachability] = [:]
    private(set) var unreachableNow: [String: NodeReachability] = [:]

    @ObservationIgnored private var featuresGeneration = 0

    /// Discovered members set aside with "Not now", per context, until the app restarts.
    @ObservationIgnored private var dismissedByContext: [String: Set<String>] = [:]

    /// Whether node's Talos version has `feature`; supported while its features are unknown.
    func support(_ feature: NodeFeature, node: String) -> FeatureSupport {
        featureSupport(nodeFeatures[node], feature)
    }

    /// Support of a cluster-wide feature across the nodes whose features are known.
    func clusterSupport(_ feature: NodeFeature) -> FeatureSupport {
        IchorCore.clusterSupport(Array(nodeFeatures.values), feature)
    }

    /// Loads node's features unless they are cached for `version` (the node's Talos version
    /// when the caller knows it: an upgraded node is asked again). Failures are silent: an
    /// unknown node counts as supporting everything and answers for itself.
    func loadFeatures(node: String, version: String = "") async {
        if let cached = nodeFeatures[node],
            version.isEmpty || cached.version.isEmpty
                || compareTalosVersions(cached.version, version) == 0
        {
            return
        }
        guard let client = model?.client else { return }
        let generation = featuresGeneration
        do {
            let loaded = try await client.features(node: node)
            guard generation == featuresGeneration else { return }
            nodeFeatures[node] = loaded
            unreachableNow[node] = nil
        } catch {
            // The only live word the hub has on a node that went down after the last overview.
            guard generation == featuresGeneration else { return }
            unreachableNow[node] = NodeReachability.from(failure: error.localizedDescription)
        }
    }

    /// Whether node answers: the last overview's word (in memory, else the last known one),
    /// overruled by its features call failing to dial it. Nil while unknown: nothing is dimmed.
    func reachability(of node: String) -> NodeReachability? {
        let known =
            nodeReachability[node]
            ?? model?.lastKnown(.overview, as: ClusterOverview.self)?.value.reachability(of: node)
        return NodeReachability.merge(known: known, live: unreachableNow[node])
    }

    /// Records what node's sensors said, for its row in the node lists.
    func setThrottled(node: String, _ throttled: Bool) {
        if throttled { throttledNodes.insert(node) } else { throttledNodes.remove(node) }
    }

    /// Drops node's cached features and asks again: its Talos version just changed (upgrade).
    func reloadFeatures(node: String) async {
        nodeFeatures[node] = nil
        await loadFeatures(node: node)
    }

    /// The features of every reachable node of an overview, in parallel.
    /// Also records their MACs for Wake-on-LAN (recordNodeMacs), alongside.
    func loadFeatures(of nodes: [NodeOverview]) async {
        Task { await model?.recordNodeMacs(of: nodes) }
        for node in nodes {
            nodeReachability[node.node] = NodeReachability(
                reachable: node.reachable, errorKind: node.errorKind, error: node.error, lastSeen: node.lastSeen)
        }
        await withTaskGroup(of: Void.self) { group in
            for node in nodes where node.reachable {
                group.addTask { await self.loadFeatures(node: node.node, version: node.version) }
            }
        }
    }

    /// Other context, other config or other (masked) node names: the cache no longer applies.
    func forget() {
        featuresGeneration += 1
        nodeFeatures = [:]
        throttledNodes = []
        nodeReachability = [:]
        unreachableNow = [:]
    }

    func dismissedNodes(of context: String) -> Set<String> { dismissedByContext[context] ?? [] }

    func dismissNodes(_ nodes: [DiscoveredNode], of context: String) {
        dismissedByContext[context, default: []].formUnion(nodes.map(\.address))
    }

    /// Addresses and hostnames just taken out of a context: a member lingers for a while, and
    /// discovery must not offer them back.
    func noteForgotten(_ names: [String], of context: String) {
        dismissedByContext[context, default: []].formUnion(names.filter { !$0.isEmpty })
    }
}

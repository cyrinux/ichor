import Foundation

// "Last known state": what the app shows of a cluster it cannot reach. Same rules as Android.

/// `current` with each unreachable node filled in from `previous` (the overview shown before,
/// fetched at `previousAt`) when the same node answered then, or was itself filled in: its
/// identity and capacity, and when it was last seen. What `current` says about the node now
/// (unreachable, the error, readiness, free memory) is kept. Reachable nodes are left as
/// they are.
public func mergeLastKnown(current: ClusterOverview, previous: ClusterOverview?, previousAt: Date) -> ClusterOverview {
    guard let previous else { return current }
    let known = Dictionary(previous.nodes.map { ($0.node, $0) }, uniquingKeysWith: { first, _ in first })
    let previousMillis = Int64((previousAt.timeIntervalSince1970 * 1000).rounded())
    let nodes = current.nodes.map { node -> NodeOverview in
        guard !node.reachable, let last = known[node.node], last.reachable || last.lastSeen != nil else { return node }
        return NodeOverview(
            node: node.node, hostname: last.hostname, reachable: false, error: node.error, errorKind: node.errorKind,
            version: last.version, arch: last.arch, platform: last.platform, role: last.role, stage: node.stage,
            ready: node.ready, unmetConditions: node.unmetConditions, cpuCount: last.cpuCount, memTotal: last.memTotal,
            memAvailable: node.memAvailable, lastSeen: last.lastSeen ?? previousMillis
        )
    }
    return ClusterOverview(context: current.context, nodes: nodes)
}

extension ClusterOverview {
    /// No node answers, but some were seen before: the nodes are listed as last known, under
    /// a banner, instead of the outage notice.
    public var showsLastKnown: Bool {
        outage != nil && nodes.contains { $0.lastSeen != nil }
    }
}

extension NodeOverview {
    /// When an unreachable node last answered, nil when unknown.
    public var lastSeenDate: Date? {
        lastSeen.map { Date(epochMillis: $0) }
    }
}

/// What may be kept on the phone for a cluster: the screens worth showing offline. Never logs,
/// live stats, processes, connections, machine config, resources, kubeconfig or time.
public enum LastKnownDomain: Hashable, Sendable {
    case overview, etcd, kubespan, inventory, workloads, pods
    case services(node: String)
    case resources(node: String)
    case hardware(node: String)
    case network(node: String)
    case images(node: String)

    /// Unique per cluster; hashed with the cluster's fingerprint into the file name.
    public var key: String {
        switch self {
        case .overview: "overview"
        case .etcd: "etcd"
        case .kubespan: "kubespan"
        case .inventory: "inventory"
        case .workloads: "workloads"
        case .pods: "pods"
        case .services(let node): "services/\(node)"
        case .resources(let node): "resources/\(node)"
        case .hardware(let node): "hardware/\(node)"
        case .network(let node): "network/\(node)"
        case .images(let node): "images/\(node)"
        }
    }
}

/// A stored fetch: the raw JSON of the domain `key`, fetched at `at` (epoch ms).
public struct LastKnownEntry: Codable, Equatable, Sendable {
    public let key: String
    public let at: Int64
    public let json: String

    public init(key: String, at: Date, json: String) {
        self.key = key
        self.at = Int64((at.timeIntervalSince1970 * 1000).rounded())
        self.json = json
    }

    public var date: Date { Date(epochMillis: at) }

    /// Older than lastKnownMaxAge is too old to show; so is one from the future beyond a few
    /// minutes (the clock was moved back), whose age cannot be told.
    public func isFresh(now: Date) -> Bool {
        let age = now.timeIntervalSince(date)
        return age > -300 && age < lastKnownMaxAge
    }
}

/// How long the last known state is kept on the phone.
public let lastKnownMaxAge: TimeInterval = 24 * 3_600

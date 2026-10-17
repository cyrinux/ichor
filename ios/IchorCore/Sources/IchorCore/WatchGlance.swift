import Foundation

// Apple Watch (CYR-339): the phone sends the watch what its widget shows, read-only, over
// WatchConnectivity. Only a WatchPayload crosses: cluster names as on screen (masked under the
// screenshot mode, like the widget's list), node hostnames, states and reasons with addresses
// scrubbed, and etcd alarm counts. No config, token, endpoint, context name or monitor key.

/// A node on the watch: its hostname (empty when unknown; never its address), state and reason.
public struct WatchNode: Codable, Equatable, Sendable {
    public var name: String
    public var health: NodeHealth
    public var reason: String

    public init(name: String, health: NodeHealth, reason: String = "") {
        self.name = name
        self.health = health
        self.reason = reason
    }
}

/// A cluster on the watch: its name, whether it is the one open on the phone, and its last check
/// (nil before one was made).
public struct WatchCluster: Codable, Equatable, Sendable, Identifiable {
    /// Its position in the phone's list ("0", "1"…): a stable list id that names nothing.
    public var id: String
    public var name: String
    public var active: Bool
    public var takenAt: Date?
    public var nodes: [WatchNode]
    public var etcdChecked: Bool
    public var etcdAlarms: Int

    public init(id: String, name: String, active: Bool, takenAt: Date? = nil, nodes: [WatchNode] = [],
                etcdChecked: Bool = false, etcdAlarms: Int = 0) {
        self.id = id
        self.name = name
        self.active = active
        self.takenAt = takenAt
        self.nodes = nodes
        self.etcdChecked = etcdChecked
        self.etcdAlarms = etcdAlarms
    }

    /// The same check as a ClusterSnapshot (nodes by position), so the watch reuses the widget's
    /// rules (glanceItems, isGlanceStale); nil before the first check.
    public var snapshot: ClusterSnapshot? {
        guard let takenAt else { return nil }
        var byKey: [String: NodeState] = [:]
        for (i, node) in nodes.enumerated() {
            byKey[String(i)] = NodeState(hostname: node.name, health: node.health, reason: node.reason)
        }
        return ClusterSnapshot(context: name, takenAt: takenAt, nodes: byKey,
                               etcdAlarms: Array(repeating: "", count: etcdAlarms), etcdChecked: etcdChecked)
    }

    /// The widget's status items (at most two), empty before the first check.
    public var glance: [GlanceItem] { snapshot.map(glanceItems) ?? [] }

    /// The worst tone of its items; nil before the first check.
    public var tone: GlanceItem.Tone? {
        let tones = glance.map(\.tone)
        if tones.isEmpty { return nil }
        if tones.contains(.problem) { return .problem }
        return tones.contains(.warning) ? .warning : .ok
    }

    public var readyCount: Int { nodes.filter { $0.health == .ready }.count }

    /// Nodes not ready or unreachable: the "N down" of the list.
    public var downCount: Int { nodes.count - readyCount }

    public func isStale(now: Date) -> Bool {
        snapshot.map { isGlanceStale($0, now: now) } ?? true
    }
}

/// Everything the watch gets, as one application context (only the latest one is kept).
public struct WatchPayload: Codable, Equatable, Sendable {
    /// The application context's key, holding this payload's JSON.
    public static let contextKey = "watchPayload"
    /// Where the watch app keeps the last one for its complication (its own App Group defaults).
    public static let storeKey = "watchPayload"

    public var sentAt: Date
    /// The screenshot mode was on: the names are the masked ones.
    public var masked: Bool
    public var clusters: [WatchCluster]

    public init(sentAt: Date, masked: Bool, clusters: [WatchCluster]) {
        self.sentAt = sentAt
        self.masked = masked
        self.clusters = clusters
    }

    /// The cluster the complication shows: the one open on the phone, else the first.
    public var activeCluster: WatchCluster? { clusters.first { $0.active } ?? clusters.first }

    public func encoded() -> Data? { try? JSONEncoder().encode(self) }

    public static func decode(_ data: Data?) -> WatchPayload? {
        data.flatMap { try? JSONDecoder().decode(WatchPayload.self, from: $0) }
    }

    public static func load(from defaults: UserDefaults?) -> WatchPayload? {
        decode(defaults?.data(forKey: storeKey))
    }
}

/// updateApplicationContext's practical ceiling is about 64 KB: stay under it.
public let watchPayloadLimit = 60 * 1024
/// A reason longer than this is cut on the watch.
public let watchReasonLength = 120

/// A snapshot's nodes for the watch: the ones down first (unreachable, then not ready), then by
/// name; reasons cut to `watchReasonLength`, with their addresses scrubbed.
public func watchRows(from snapshot: ClusterSnapshot) -> [WatchNode] {
    let rank: (NodeHealth) -> Int = { health in
        switch health {
        case .unreachable: return 0
        case .notReady: return 1
        case .ready: return 2
        }
    }
    let rows = snapshot.nodes.values.map { state in
        // A hostname that is an address is no name to show: the watch numbers that node.
        WatchNode(name: scrubbedAddresses(state.hostname) == state.hostname ? state.hostname : "",
                  health: state.health,
                  reason: cut(scrubbedAddresses(state.reason), to: watchReasonLength))
    }
    return rows.sorted {
        rank($0.health) != rank($1.health)
            ? rank($0.health) < rank($1.health)
            : $0.name.localizedStandardCompare($1.name) == .orderedAscending
    }
}

/// The payload of the widget's data: `clusters` in the phone's order (WidgetCluster, named as on
/// screen), `snapshots` by monitor key, `active` the active cluster's key. Trimmed to `limit`
/// bytes of JSON (see trimmedWatchPayload).
public func watchPayload(clusters: [WidgetCluster], snapshots: [String: ClusterSnapshot], active: String?,
                         masked: Bool, sentAt: Date, limit: Int = watchPayloadLimit) -> WatchPayload {
    let rows = clusters.enumerated().map { index, cluster -> WatchCluster in
        let snapshot = snapshots[cluster.id]
        return WatchCluster(id: String(index), name: cluster.name, active: cluster.id == active,
                            takenAt: snapshot?.takenAt, nodes: snapshot.map(watchRows) ?? [],
                            etcdChecked: snapshot?.etcdChecked ?? false, etcdAlarms: snapshot?.etcdAlarms.count ?? 0)
    }
    return trimmedWatchPayload(WatchPayload(sentAt: sentAt, masked: masked, clusters: rows), limit: limit)
}

/// `payload` within `limit` bytes of JSON, giving up the least useful first: reasons cut to 40
/// characters, then no reasons, then the ready nodes of the clusters not active (their counts
/// go with them), then those clusters from the end of the list. The active cluster stays.
public func trimmedWatchPayload(_ payload: WatchPayload, limit: Int = watchPayloadLimit) -> WatchPayload {
    let fits: (WatchPayload) -> Bool = { ($0.encoded()?.count ?? .max) <= limit }
    if fits(payload) { return payload }
    let steps: [(WatchNode, Bool) -> WatchNode?] = [
        { node, _ in WatchNode(name: node.name, health: node.health, reason: cut(node.reason, to: 40)) },
        { node, _ in WatchNode(name: node.name, health: node.health) },
        { node, active in active || node.health != .ready ? node : nil },
    ]
    var trimmed = payload
    for step in steps {
        trimmed.clusters = trimmed.clusters.map { cluster in
            var copy = cluster
            copy.nodes = cluster.nodes.compactMap { step($0, cluster.active) }
            return copy
        }
        if fits(trimmed) { return trimmed }
    }
    while fits(trimmed) == false, let last = trimmed.clusters.lastIndex(where: { !$0.active }) {
        trimmed.clusters.remove(at: last)
    }
    // Only the active cluster left and still too big: its first nodes (the ones down come first).
    while fits(trimmed) == false, let index = trimmed.clusters.indices.first, !trimmed.clusters[index].nodes.isEmpty {
        trimmed.clusters[index].nodes.removeLast()
    }
    return trimmed
}

/// `text` without IP addresses (v4 with an optional port, v6, bracketed or not) or URLs: each
/// becomes "…". Node errors name the endpoint they could not reach; the watch gets none.
public func scrubbedAddresses(_ text: String) -> String {
    guard !text.isEmpty else { return text }
    var result = text
    for pattern in addressPatterns {
        guard let regex = try? NSRegularExpression(pattern: pattern) else { continue }
        let range = NSRange(result.startIndex..., in: result)
        result = regex.stringByReplacingMatches(in: result, range: range, withTemplate: "…")
    }
    return result
}

private let addressPatterns = [
    #"[a-zA-Z][a-zA-Z0-9+.-]*://[^\s"']+"#,
    #"\[[0-9A-Fa-f.]*:[0-9A-Fa-f:.]*(?:%\w+)?\](?::\d+)?"#,
    #"(?<![\w.])\d{1,3}(?:\.\d{1,3}){3}(?::\d+)?(?![\w.])"#,
    #"(?<![\w:])(?:[0-9A-Fa-f]{0,4}:){2,7}[0-9A-Fa-f]{1,4}(?![\w:])"#,
]

private func cut(_ text: String, to length: Int) -> String {
    text.count <= length ? text : String(text.prefix(length - 1)) + "…"
}

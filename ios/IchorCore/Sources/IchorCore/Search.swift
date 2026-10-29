import Foundation

/// One cached blob the Go core's SearchSnapshots ranks: the raw JSON of `key` ("overview",
/// "pods|*"…) of cluster `cluster` (its id, else its fingerprint), fetched at `at` (epoch ms).
public struct SearchEntry: Codable, Equatable, Sendable {
    public let cluster: String
    public let context: String
    public let key: String
    public let at: Int64
    public let json: String

    public init(cluster: String, context: String, key: String, at: Int64, json: String) {
        self.cluster = cluster
        self.context = context
        self.key = key
        self.at = at
        self.json = json
    }

    public init(cluster: String, context: String, key: String, at: Date, json: String) {
        self.init(
            cluster: cluster, context: context, key: key, at: Int64((at.timeIntervalSince1970 * 1000).rounded()),
            json: json)
    }
}

/// The JSON SearchSnapshots and DemoSearchEntries take and give: {"entries":[…]}.
public struct SearchEntries: Codable, Equatable, Sendable {
    public let entries: [SearchEntry]

    public init(entries: [SearchEntry]) { self.entries = entries }

    public var json: String { (try? TalosJSON.encode(self)) ?? #"{"entries":[]}"# }
}

/// One match: a node, pod, workload, CronJob, Argo CD or Flux app of a cluster. `target` opens
/// its screen (nil when a share link could not name it: the row still shows, without a tap).
public struct SearchResult: Decodable, Equatable, Identifiable, Sendable {
    public let cluster: String
    public let context: String
    /// node, pod, workload, cronjob, argo-app or flux-app.
    public let kind: String
    public let namespace: String
    public let name: String
    /// One line in English, e.g. "Deployment · 2/3 ready".
    public let detail: String
    /// When the blob it comes from was fetched (epoch ms).
    public let at: Int64
    public let score: Double
    public let target: ShareTarget?

    public var id: String { "\(cluster)|\(kind)|\(namespace)|\(name)" }
    public var date: Date { Date(epochMillis: at) }

    private enum CodingKeys: String, CodingKey {
        case cluster, context, kind, namespace, name, detail, at, score, target
    }

    // Go leaves out an empty namespace and a missing target.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        cluster = try c.field(.cluster, "")
        context = try c.field(.context, "")
        kind = try c.field(.kind, "")
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        detail = try c.field(.detail, "")
        at = try c.field(.at, 0)
        score = try c.field(.score, 0)
        target = try c.decodeIfPresent(ShareTarget.self, forKey: .target)
    }
}

public struct SearchResults: Decodable, Equatable, Sendable {
    public let results: [SearchResult]
    /// Matches before the limit.
    public let total: Int

    public static let empty = SearchResults(results: [], total: 0)

    public init(results: [SearchResult], total: Int) {
        self.results = results
        self.total = total
    }

    private enum CodingKeys: String, CodingKey { case results, total }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        results = try c.field(.results, [])
        total = try c.field(.total, 0)
    }
}

/// The last known keys search reads, by kind, best first: a list of every namespace before the
/// summary key. Lists of one namespace are kept under keys that cannot be listed: not read.
public let searchKeys: [[String]] = [
    ["overview"],
    [kubeListKey("pods", namespace: nil), LastKnownDomain.pods.key],
    [kubeListKey("workloads", namespace: nil), LastKnownDomain.workloads.key],
    [kubeListKey("cronjobs", namespace: nil), LastKnownDomain.cronJobs.key],
]

/// The id search groups a context under: its cluster id, else its fingerprint (no share link then).
public func searchCluster(of context: ContextSummary) -> String {
    context.clusterID.isEmpty ? context.fingerprint : context.clusterID
}

/// The blobs to search: for each cluster (the active one first), the first of each kind of
/// `searchKeys` that `read(fingerprint, key)` has among its contexts (the active context first),
/// then what `active` adds for the active cluster (its Argo CD and Flux apps, what a refresh
/// fetched). A kind `active` gives replaces the stored one. Demo contexts give nothing here:
/// the caller adds DemoSearchEntries.
public func searchEntries(
    contexts: [ContextSummary],
    activeContext: String,
    read: (_ fingerprint: String, _ key: String) -> LastKnownEntry?,
    active: [SearchEntry] = []
) -> [SearchEntry] {
    let real = contexts.filter { !$0.demo && !$0.fingerprint.isEmpty }
    let activeCluster = real.first { $0.name == activeContext }.map(searchCluster)
    var clusters: [String] = []
    var byCluster: [String: [ContextSummary]] = [:]
    for context in real.sorted(by: { ($0.name == activeContext ? 0 : 1) < ($1.name == activeContext ? 0 : 1) }) {
        let cluster = searchCluster(of: context)
        if byCluster[cluster] == nil { clusters.append(cluster) }
        byCluster[cluster, default: []].append(context)
    }
    let activeKinds = Set(active.map { searchKind(of: $0.key) })
    var out: [SearchEntry] = []
    for cluster in clusters {
        for keys in searchKeys {
            if cluster == activeCluster, activeKinds.contains(searchKind(of: keys[0])) { continue }
            if let found = first(in: byCluster[cluster] ?? [], keys: keys, read: read) {
                out.append(
                    SearchEntry(
                        cluster: cluster, context: found.context.name, key: found.entry.key, at: found.entry.at,
                        json: found.entry.json))
            }
        }
        if cluster == activeCluster { out += active }
    }
    return out
}

private func first(
    in contexts: [ContextSummary], keys: [String],
    read: (String, String) -> LastKnownEntry?
) -> (context: ContextSummary, entry: LastKnownEntry)? {
    for key in keys {
        for context in contexts {
            if let entry = read(context.fingerprint, key) { return (context, entry) }
        }
    }
    return nil
}

/// The kind a cache key holds: "pods|*" and "pods" are pods (as the Go core reads them).
public func searchKind(of key: String) -> String {
    String(key.prefix { $0 != "|" && $0 != "/" })
}

/// The results grouped by cluster, the active one first, then in the order of their best match.
public func searchSections(_ results: [SearchResult], activeCluster: String) -> [(
    cluster: String, context: String, results: [SearchResult]
)] {
    var order: [String] = []
    var grouped: [String: [SearchResult]] = [:]
    for result in results {
        if grouped[result.cluster] == nil { order.append(result.cluster) }
        grouped[result.cluster, default: []].append(result)
    }
    if let index = order.firstIndex(of: activeCluster) { order.insert(order.remove(at: index), at: 0) }
    return order.map { cluster in
        let rows = grouped[cluster] ?? []
        return (cluster, rows.first?.context ?? "", rows)
    }
}

/// Whether a result's data is old enough to say when it was seen (more than 5 minutes).
public func searchIsStale(_ result: SearchResult, now: Date) -> Bool {
    now.timeIntervalSince(result.date) > 300
}

/// How many recent searches are kept.
public let searchRecentLimit = 10

/// `recent` with `query` first (trimmed, once whatever its case), at most searchRecentLimit.
public func searchRecents(adding query: String, to recent: [String]) -> [String] {
    let query = query.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !query.isEmpty else { return recent }
    let rest = recent.filter { $0.caseInsensitiveCompare(query) != .orderedSame }
    return Array(([query] + rest).prefix(searchRecentLimit))
}

/// The fields the Go core reads of an Argo CD or Flux app ({"apps":[…]}): the stores keep the
/// decoded apps only, so they are encoded again for search.
public struct SearchApp: Encodable, Equatable, Sendable {
    public let kind: String
    public let namespace: String
    public let name: String
    public let project: String
    public let level: String
    public let health: String
}

private struct SearchApps: Encodable { let apps: [SearchApp] }

extension ServiceHealth {
    /// The Go core's name of the level.
    public var wire: String {
        switch self {
        case .critical: "critical"
        case .warning: "warning"
        case .unknown: "unknown"
        case .ok: "ok"
        case .idle: "idle"
        }
    }
}

extension ArgoStatus {
    /// The apps as SearchSnapshots reads them, under the "argocd" key.
    public var searchJSON: String {
        let apps = apps.map {
            SearchApp(
                kind: "Application", namespace: $0.namespace, name: $0.name, project: $0.project, level: $0.level.wire,
                health: $0.health.rawValue)
        }
        return (try? TalosJSON.encode(SearchApps(apps: apps))) ?? #"{"apps":[]}"#
    }
}

extension FluxStatus {
    /// The apps as SearchSnapshots reads them, under the "flux" key.
    public var searchJSON: String {
        let apps = apps.map {
            SearchApp(
                kind: $0.kind, namespace: $0.namespace, name: $0.name, project: "", level: $0.level.wire, health: "")
        }
        return (try? TalosJSON.encode(SearchApps(apps: apps))) ?? #"{"apps":[]}"#
    }
}

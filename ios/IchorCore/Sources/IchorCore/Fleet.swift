import Foundation

// The fleet view: every cluster's last background check on one list, worst first. The app
// builds one FleetEntry per cluster from the snapshots the monitor keeps (fleetEntries), the Go
// core's FleetSummary ranks them, and FleetView draws the rows. Same rules as Android
// (monitor/FleetInput.kt, model/Fleet.kt).

/// A node of a FleetEntry: its hostname (empty in screenshot mode) and health, the raw
/// NodeHealth (ready, notReady, unreachable).
public struct FleetNodeEntry: Encodable, Equatable, Sendable {
    public let hostname: String
    public let health: String
}

/// One cluster's last check as FleetSummary reads it. `takenAt` is unix ms written by hand (the
/// default Date encoding is seconds since 2001): 0 when never checked.
public struct FleetEntry: Encodable, Equatable, Sendable {
    public let key: String
    public let name: String
    public let context: String
    public let kube: Bool
    public let tags: [String]
    public let takenAt: Int64
    public let failures: Int
    public let nodes: [String: FleetNodeEntry]
    public let etcdChecked: Bool
    public let etcdAlarms: [String]
    public let amChecked: Bool
    public let amIssues: [String: String]
    public let checkupChecked: Bool
    public let checkupIssues: [String: String]
    public let dataChecked: Bool
    public let dataIssues: [String: String]
    public let gitopsChecked: Bool
    public let gitopsIssues: [String: String]
    public let storageChecked: Bool
    public let storageIssues: [String: String]
    public let sensorChecked: Bool
    public let sensorIssues: [String: String]
    public let kubespanChecked: Bool
    public let kubespanIssues: [String: String]
    public let pluginChecked: Bool
    public let pluginIssues: [String: String]

    /// `snapshot` nil: a cluster not checked yet.
    init(
        key: String, name: String, context: String, kube: Bool, snapshot: ClusterSnapshot?, failures: Int,
        masked: Bool, tags: [String] = []
    ) {
        self.key = key
        self.name = name
        self.context = context
        self.kube = kube || snapshot?.kube == true
        self.tags = tags
        takenAt = snapshot.map { Int64(($0.takenAt.timeIntervalSince1970 * 1000).rounded()) } ?? 0
        self.failures = failures
        nodes = (snapshot?.nodes ?? [:]).mapValues {
            FleetNodeEntry(hostname: masked ? "" : $0.hostname, health: $0.health.rawValue)
        }
        etcdChecked = snapshot?.etcdChecked ?? false
        etcdAlarms = snapshot?.etcdAlarms ?? []
        amChecked = snapshot?.amChecked ?? false
        amIssues = snapshot?.amIssues ?? [:]
        checkupChecked = snapshot?.checkupChecked ?? false
        checkupIssues = snapshot?.checkupIssues ?? [:]
        dataChecked = snapshot?.dataChecked ?? false
        dataIssues = snapshot?.dataIssues ?? [:]
        gitopsChecked = snapshot?.gitopsChecked ?? false
        gitopsIssues = snapshot?.gitopsIssues ?? [:]
        storageChecked = snapshot?.storageChecked ?? false
        storageIssues = snapshot?.storageIssues ?? [:]
        sensorChecked = snapshot?.sensorChecked ?? false
        sensorIssues = snapshot?.sensorIssues ?? [:]
        kubespanChecked = snapshot?.kubespanChecked ?? false
        kubespanIssues = snapshot?.kubespanIssues ?? [:]
        pluginChecked = snapshot?.pluginChecked ?? false
        pluginIssues = snapshot?.pluginIssues ?? [:]
    }
}

/// The JSON FleetSummary takes: {"clusters":[…]}.
public struct FleetEntries: Encodable, Sendable {
    public let clusters: [FleetEntry]

    public init(clusters: [FleetEntry]) { self.clusters = clusters }

    public var json: String {
        let encoder = JSONEncoder()
        encoder.outputFormatting = .sortedKeys
        return (try? encoder.encode(self)).map { String(decoding: $0, as: UTF8.self) } ?? #"{"clusters":[]}"#
    }
}

/// Whether the only clusters on the phone are the demo: the fleet screen shows the demo's fleet then.
public func demoFleetOnly(_ contexts: [ContextSummary]) -> Bool {
    !contexts.isEmpty && contexts.allSatisfy(\.demo)
}

/// The fleet screen's input: one entry per cluster of `contexts` (the demo left out: its own
/// snapshot exists only while it is on screen), keyed and stored by monitorClusterKey, from the
/// snapshot the monitor kept; a cluster with none yet has takenAt 0 (never checked). Names as
/// the app shows them (`labels`); in screenshot mode the hostnames are left out too. `tags` by
/// fingerprint, the user's words: shown even in screenshot mode.
public func fleetEntries(
    snapshots: [String: ClusterSnapshot], reachability: [String: Reachability], contexts: [ContextSummary],
    labels: ClusterLabels, tags: [String: [String]] = [:]
) -> [FleetEntry] {
    var seen = Set<String>()
    return contexts.filter { !$0.demo }.compactMap { context in
        let key = monitorClusterKey(context)
        guard seen.insert(key).inserted else { return nil }
        return FleetEntry(
            key: key, name: labels.of(context), context: context.name, kube: context.isKube,
            snapshot: snapshots[key], failures: reachability[key]?.misses ?? 0, masked: labels.masked,
            tags: clusterTagsOf(saved: tags, fingerprints: clusterFingerprints(of: context, contexts: contexts)))
    }
}

/// A row's tone, worst first: down or critical, degraded, not checked lately, fine.
public enum FleetTone: String, WireEnum, Sendable, CaseIterable {
    case bad, warn, unknown, ok

    public static let wireFallback: FleetTone = .unknown
}

/// The fleet screen's list (FleetSummary): the rows worst first, the tone counts, the worst
/// tone (unknown for an empty fleet) and one group per tag (the filter chips).
public struct FleetSummary: Decodable, Equatable, Sendable {
    public let clusters: [FleetRow]
    public let counts: FleetCounts
    public let worst: FleetTone
    public let groups: [FleetGroup]

    private enum CodingKeys: String, CodingKey { case clusters, counts, worst, groups }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        clusters = try c.field(.clusters, [])
        counts = try c.field(.counts, FleetCounts())
        worst = try c.wire(.worst)
        groups = try c.field(.groups, [])
    }
}

/// One cluster's last check: `context` is the context to switch to, `checkedAt` when it ran
/// (unix ms, 0: never), `stale` when it is over an hour old (its counts stay on the row).
public struct FleetRow: Decodable, Equatable, Identifiable, Sendable {
    public let key: String
    public let name: String
    public let context: String
    public let kube: Bool
    public let tags: [String]
    public let tone: FleetTone
    public let stale: Bool
    public let checkedAt: Int64
    public let nodes: Int
    public let ready: Int
    public let notReady: Int
    public let unreachable: Int
    public let etcdAlarms: Int
    public let critical: Int
    public let warnings: Int
    public let tracksChecked: [String]
    public let topFinding: FleetFinding?

    public var id: String { key }
    /// When it was checked; nil when never.
    public var checkedDate: Date? { checkedAt > 0 ? Date(epochMillis: checkedAt) : nil }

    private enum CodingKeys: String, CodingKey {
        case key, name, context, kube, tags, tone, stale, checkedAt, nodes, ready, notReady, unreachable
        case etcdAlarms, critical, warnings, tracksChecked, topFinding
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        key = try c.field(.key, "")
        name = try c.field(.name, "")
        context = try c.field(.context, "")
        kube = try c.field(.kube, false)
        tags = try c.field(.tags, [])
        tone = try c.wire(.tone)
        stale = try c.field(.stale, false)
        checkedAt = try c.field(.checkedAt, 0)
        nodes = try c.field(.nodes, 0)
        ready = try c.field(.ready, 0)
        notReady = try c.field(.notReady, 0)
        unreachable = try c.field(.unreachable, 0)
        etcdAlarms = try c.field(.etcdAlarms, 0)
        critical = try c.field(.critical, 0)
        warnings = try c.field(.warnings, 0)
        tracksChecked = try c.field(.tracksChecked, [])
        topFinding = try c.decodeIfPresent(FleetFinding.self, forKey: .topFinding)
    }
}

/// The one thing a row says is wrong: `track` (nodes, reach, etcd, alertmanager, checkup, data,
/// gitops, storage, sensor, plugin), `severity` (critical or warning), `subject` in the core's
/// words ("node-b unreachable"), `detail` the GitOps tool (argocd, flux) or empty.
public struct FleetFinding: Decodable, Equatable, Sendable {
    public let track: String
    public let severity: String
    public let subject: String
    public let detail: String

    public init(track: String, severity: String = "warning", subject: String = "", detail: String = "") {
        self.track = track
        self.severity = severity
        self.subject = subject
        self.detail = detail
    }

    public var critical: Bool { severity == "critical" }

    /// Whether `subject` may be shown: in screenshot mode only the generic ones (a node count, an
    /// etcd alarm), since the others carry the alert, namespace or app names the check read.
    public func subjectShown(masked: Bool) -> Bool {
        !subject.isEmpty && (!masked || ["nodes", "reach", "etcd"].contains(track))
    }

    private enum CodingKeys: String, CodingKey { case track, severity, subject, detail }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        track = try c.field(.track, "")
        severity = try c.field(.severity, "warning")
        subject = try c.field(.subject, "")
        detail = try c.field(.detail, "")
    }
}

public struct FleetCounts: Decodable, Equatable, Sendable {
    public let bad: Int
    public let warn: Int
    public let unknown: Int
    public let ok: Int

    public init(bad: Int = 0, warn: Int = 0, unknown: Int = 0, ok: Int = 0) {
        self.bad = bad
        self.warn = warn
        self.unknown = unknown
        self.ok = ok
    }

    /// Rows that need a look (bad or warn).
    public var needAttention: Int { bad + warn }

    private enum CodingKeys: String, CodingKey { case bad, warn, unknown, ok }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            bad: try c.field(.bad, 0), warn: try c.field(.warn, 0), unknown: try c.field(.unknown, 0),
            ok: try c.field(.ok, 0))
    }
}

/// The clusters with `tag` ("" for the untagged ones), by `keys` in ranked order.
public struct FleetGroup: Decodable, Equatable, Sendable {
    public let tag: String
    public let counts: FleetCounts
    public let keys: [String]

    private enum CodingKeys: String, CodingKey { case tag, counts, keys }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        tag = try c.field(.tag, "")
        counts = try c.field(.counts, FleetCounts())
        keys = try c.field(.keys, [])
    }
}

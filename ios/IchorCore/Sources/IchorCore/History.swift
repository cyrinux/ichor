import Foundation

// The 30-day history ring (CYR-39): the Go core keeps its format (HistoryAppend, HistoryQuery,
// HistorySince), the app seals the bytes. After each background run, each cluster checked adds
// one record built here from what the run really read: never from screenshot-mode names.

/// One node in a record: the snapshot's health, its version and memory when known.
public struct HistoryNodeRecord: Codable, Equatable, Sendable {
    public let node: String
    public let hostname: String
    /// "ready", "notReady" or "unreachable" (NodeHealth's raw value).
    public let health: String
    public let version: String?
    public let memUsedPercent: Double?

    public init(node: String, hostname: String, health: NodeHealth, version: String? = nil, memUsedPercent: Double? = nil) {
        self.node = node
        self.hostname = hostname
        self.health = health.rawValue
        self.version = version
        self.memUsedPercent = memUsedPercent
    }
}

/// A volume's fill in a record (storage watch on): every volume read, not only those over a threshold.
public struct HistoryVolumeRecord: Codable, Equatable, Sendable {
    public let key: String
    public let name: String
    public let node: String
    public let usedPercent: Double

    public init(key: String, name: String, node: String, usedPercent: Double) {
        self.key = key
        self.name = name
        self.node = node
        self.usedPercent = usedPercent
    }
}

/// An issue still open after the run: `key` the snapshot's, `track` where it comes from.
public struct HistoryAlertRecord: Codable, Equatable, Sendable {
    public let key: String
    /// etcd, data, gitops, checkup, am, storage or cert.
    public let track: String
    public let severity: String?
    public let title: String?

    public init(key: String, track: String, severity: String? = nil, title: String? = nil) {
        self.key = key
        self.track = track
        self.severity = severity
        self.title = title
    }
}

/// What one run sends to HistoryAppend.
public struct HistoryRecord: Codable, Equatable, Sendable {
    /// Epoch milliseconds.
    public let at: Int64
    /// False when the cluster as a whole did not answer: a gap, never downtime.
    public let reachable: Bool
    public let nodes: [HistoryNodeRecord]
    public let volumes: [HistoryVolumeRecord]
    public let alerts: [HistoryAlertRecord]

    /// The JSON HistoryAppend takes.
    public func json() throws -> String {
        let encoder = JSONEncoder()
        encoder.outputFormatting = .sortedKeys
        return String(decoding: try encoder.encode(self), as: UTF8.self)
    }
}

/// What a run read beyond the snapshot: node versions and memory, and the volumes' fill.
public struct HistoryExtras: Equatable, Sendable {
    /// Node → version; a node whose version is unknown is absent.
    public var versions: [String: String]
    /// Node → memory used (%); a node that did not report its memory is absent.
    public var memory: [String: Double]
    /// Empty when storage is not watched or could not be read.
    public var volumes: [HistoryVolumeRecord]

    public init(versions: [String: String] = [:], memory: [String: Double] = [:], volumes: [HistoryVolumeRecord] = []) {
        self.versions = versions
        self.memory = memory
        self.volumes = volumes
    }
}

/// Versions and memory of a Talos overview's nodes.
public func historyExtras(_ overview: ClusterOverview) -> HistoryExtras {
    var extras = HistoryExtras()
    for n in overview.nodes {
        if !n.version.isEmpty { extras.versions[n.node] = n.version }
        if n.memTotal > 0 {
            let used = Double(n.memTotal - min(n.memAvailable, n.memTotal))
            extras.memory[n.node] = 100 * used / Double(n.memTotal)
        }
    }
    return extras
}

/// Kubelet versions of a kubeconfig cluster's nodes (the API gives no memory use).
public func historyExtras(_ overview: KubeNodesOverview) -> HistoryExtras {
    var extras = HistoryExtras()
    for n in overview.nodes {
        if let kubelet = n.kubelet, !kubelet.isEmpty { extras.versions[n.name] = kubelet }
    }
    return extras
}

/// Every volume of the nodes that answered.
public func historyVolumes(_ health: ClusterStorageHealth) -> [HistoryVolumeRecord] {
    health.nodes.flatMap { node in
        node.volumes.map { HistoryVolumeRecord(key: $0.key, name: $0.name, node: node.node, usedPercent: $0.usedPercent) }
    }
}

/// The screenshot mode masks every name the core returns: a run under it records nothing.
public func historyRecordingAllowed(privacyMasked: Bool) -> Bool { !privacyMasked }

/// The record of a run. `current`: the fresh snapshot, nil when the cluster did not answer at all;
/// `evaluated`: the snapshot after evaluate (its issues are the notified ones); `previousAlerts`:
/// the alerts open in the ring, resent for a track that could not be read this run (and all of
/// them when the cluster did not answer).
public func historyRecord(at: Date, current: ClusterSnapshot?, evaluated: ClusterSnapshot?, extras: HistoryExtras,
                          previousAlerts: [HistoryAlertRecord], now: Date) -> HistoryRecord {
    let millis = Int64((at.timeIntervalSince1970 * 1000).rounded())
    guard let current, !current.unreachableAsAWhole else {
        let nodes = current.map { historyNodes($0, extras: extras) } ?? []
        return HistoryRecord(at: millis, reachable: false, nodes: nodes, volumes: [], alerts: sortedAlerts(previousAlerts))
    }
    let alerts = historyOpenAlerts(evaluated ?? current, previous: previousAlerts, now: now)
    return HistoryRecord(at: millis, reachable: true, nodes: historyNodes(current, extras: extras), volumes: extras.volumes, alerts: alerts)
}

private func historyNodes(_ snapshot: ClusterSnapshot, extras: HistoryExtras) -> [HistoryNodeRecord] {
    snapshot.nodes.keys.sorted().compactMap { address in
        guard let state = snapshot.nodes[address] else { return nil }
        return HistoryNodeRecord(node: address, hostname: state.hostname, health: state.health,
                                 version: extras.versions[address], memUsedPercent: extras.memory[address])
    }
}

private func sortedAlerts(_ alerts: [HistoryAlertRecord]) -> [HistoryAlertRecord] {
    alerts.sorted { ($0.track, $0.key) < ($1.track, $1.key) }
}

/// The issues open after the run, across tracks, keyed as in the snapshot. A track watched but not
/// read this run resends `previous`'s keys of that track; a track not watched sends none.
public func historyOpenAlerts(_ snapshot: ClusterSnapshot, previous: [HistoryAlertRecord], now: Date) -> [HistoryAlertRecord] {
    func resent(_ track: String) -> [HistoryAlertRecord] { previous.filter { $0.track == track } }
    var out: [HistoryAlertRecord] = []
    if !snapshot.kube {
        out += snapshot.etcdChecked ? snapshot.etcdAlarms.map(etcdAlert) : resent(HistoryTrack.etcd)
    }
    out += track(HistoryTrack.data, snapshot.dataTrack, resent: resent) { key, value in
        HistoryAlertRecord(key: key, track: HistoryTrack.data, severity: value, title: lastPart(key))
    }
    out += track(HistoryTrack.gitops, snapshot.gitopsTrack, resent: resent) { key, value in
        HistoryAlertRecord(key: key, track: HistoryTrack.gitops, severity: gitopsSeverity(value), title: GitOpsSubject(key: key).title)
    }
    out += track(HistoryTrack.checkup, snapshot.checkupTrack, resent: resent) { key, value in
        HistoryAlertRecord(key: key, track: HistoryTrack.checkup, severity: value, title: CheckupSubject(key: key).subject)
    }
    out += track(HistoryTrack.alertmanager, snapshot.amTrack, resent: resent) { key, value in
        let issue = AMIssue(value: value)
        return HistoryAlertRecord(key: key, track: HistoryTrack.alertmanager, severity: issue.severity, title: issue.alertname)
    }
    out += track(HistoryTrack.storage, snapshot.storageTrack, resent: resent) { key, value in
        let issue = StorageIssue(value: value)
        return HistoryAlertRecord(key: key, track: HistoryTrack.storage, severity: issue.severity, title: "\(issue.name) (\(issue.hostname))")
    }
    if snapshot.certNotAfter > 0 {
        let days = daysUntil(snapshot.certNotAfter, now: now)
        if days <= certWarnDays {
            out.append(HistoryAlertRecord(key: "cert", track: HistoryTrack.cert, severity: days < 0 ? dataCritical : dataWarning))
        }
    }
    return sortedAlerts(out)
}

/// The tracks a record's alerts come from (Go's names).
public enum HistoryTrack {
    public static let etcd = "etcd"
    public static let data = "data"
    public static let gitops = "gitops"
    public static let checkup = "checkup"
    public static let alertmanager = "am"
    public static let storage = "storage"
    public static let cert = "cert"
}

private func etcdAlert(_ alarm: String) -> HistoryAlertRecord {
    HistoryAlertRecord(key: alarm, track: HistoryTrack.etcd, severity: dataCritical, title: lastPart(alarm, separator: ":"))
}

private func lastPart(_ key: String, separator: Character = "|") -> String {
    String(key.split(separator: separator, maxSplits: 1).last ?? Substring(key))
}

private func track(_ name: String, _ track: IssueTrack, resent: (String) -> [HistoryAlertRecord],
                   _ alert: (String, String) -> HistoryAlertRecord) -> [HistoryAlertRecord] {
    guard track.watched else { return [] }
    guard track.checked else { return resent(name) }
    return track.issues.keys.sorted().compactMap { key in track.issues[key].map { alert(key, $0) } }
}

/// A cluster's sealed ring as the device has it.
public enum HistoryStoredRing: Equatable, Sendable {
    /// No history yet (or one whose key is gone: started over).
    case none
    case ring(Data)
    /// There but not readable now (device locked): left alone.
    case unreadable
}

/// The bytes to store after a run: `append` (Go's HistoryAppend) applied to the stored ring; nil
/// keeps what is stored: an unreadable ring, an append that throws (a ring from a newer version,
/// an invalid record) or that returns nothing.
public func historyNextRing(_ stored: HistoryStoredRing, append: (Data?) throws -> Data) -> Data? {
    let ring: Data?
    switch stored {
    case .unreadable: return nil
    case .none: ring = nil
    case .ring(let bytes): ring = bytes
    }
    guard let next = try? append(ring), !next.isEmpty else { return nil }
    return next
}

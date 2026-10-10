import Foundation

// Background alerts on node storage (opt-in, Talos clusters only, see Monitor.swift): the Go
// core's ClusterStorageHealth, which issues are worth a notification at the user's thresholds,
// and the English wording (BackgroundMonitor rebuilds it in the user's language).

/// Every node's volumes and disks (the Go core's ClusterStorageHealth).
public struct ClusterStorageHealth: Decodable, Equatable, Sendable {
    public let context: String
    public let nodes: [StorageNode]

    public init(context: String = "", nodes: [StorageNode] = []) {
        self.context = context
        self.nodes = nodes
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        context = try c.field(.context, "")
        nodes = try c.field(.nodes, [])
    }

    private enum CodingKeys: String, CodingKey { case context, nodes }
}

/// One node: its named volumes' fill and its disks' SMART verdict; `error` when it did not answer
/// (the lists are then empty).
public struct StorageNode: Decodable, Equatable, Sendable {
    /// Its address in the talosconfig: the first part of every key.
    public let node: String
    public let hostname: String
    public let volumes: [StorageVolume]
    public let disks: [StorageDisk]
    public let error: String?

    public init(node: String, hostname: String, volumes: [StorageVolume] = [], disks: [StorageDisk] = [], error: String? = nil) {
        self.node = node
        self.hostname = hostname
        self.volumes = volumes
        self.disks = disks
        self.error = error
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.field(.node, "")
        hostname = try c.field(.hostname, "")
        volumes = try c.field(.volumes, [])
        disks = try c.field(.disks, [])
        error = try c.decodeIfPresent(String.self, forKey: .error)
    }

    private enum CodingKeys: String, CodingKey { case node, hostname, volumes, disks, error }
}

public struct StorageVolume: Decodable, Equatable, Sendable {
    /// "<node>|<name>", stable across checks.
    public let key: String
    /// EPHEMERAL, STATE, a user volume's name, else its mount point.
    public let name: String
    public let mount: String
    public let usedPercent: Double
    public let freeBytes: UInt64
    public let sizeBytes: UInt64

    public init(key: String, name: String, mount: String = "", usedPercent: Double, freeBytes: UInt64 = 0, sizeBytes: UInt64 = 0) {
        self.key = key
        self.name = name
        self.mount = mount
        self.usedPercent = usedPercent
        self.freeBytes = freeBytes
        self.sizeBytes = sizeBytes
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        key = try c.field(.key, "")
        name = try c.field(.name, "")
        mount = try c.field(.mount, "")
        usedPercent = try c.field(.usedPercent, 0)
        freeBytes = try c.field(.freeBytes, 0)
        sizeBytes = try c.field(.sizeBytes, 0)
    }

    private enum CodingKeys: String, CodingKey { case key, name, mount, usedPercent, freeBytes, sizeBytes }
}

public struct StorageDisk: Decodable, Equatable, Sendable {
    /// "<node>|smart|<device>", stable across checks.
    public let key: String
    public let device: String
    public let model: String
    /// "ok", "failing" or "unknown" (no SMART data).
    public let health: String
    public let reason: String

    public init(key: String, device: String, model: String = "", health: String, reason: String = "") {
        self.key = key
        self.device = device
        self.model = model
        self.health = health
        self.reason = reason
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        key = try c.field(.key, "")
        device = try c.field(.device, "")
        model = try c.field(.model, "")
        health = try c.field(.health, "unknown")
        reason = try c.field(.reason, "")
    }

    private enum CodingKeys: String, CodingKey { case key, device, model, health, reason }
}

/// The fill thresholds the storage alerts use (the Go `level` uses the defaults).
public let storageWarnDefault = 85
public let storageCritDefault = 95
public let storageWarnRange = 50...98

/// The critical thresholds allowed above the warning one.
public func storageCritRange(warn: Int) -> ClosedRange<Int> { min(max(warn, storageWarnRange.lowerBound) + 1, 99)...99 }

/// A volume alerts as a warning from `warn` % used, as critical from `crit` %.
public struct StorageThresholds: Equatable, Sendable {
    public let warn: Int
    public let crit: Int

    /// Out-of-range values take their default; a critical threshold not above the warning one
    /// is raised to just above it.
    public init(warn: Int = storageWarnDefault, crit: Int = storageCritDefault) {
        let w = storageWarnRange.contains(warn) ? warn : storageWarnDefault
        let range = storageCritRange(warn: w)
        self.warn = w
        self.crit = range.contains(crit) ? crit : max(range.lowerBound, min(storageCritDefault, range.upperBound))
    }

    /// dataCritical, dataWarning or nil for a volume `usedPercent` full.
    public func severity(usedPercent: Double) -> String? {
        if usedPercent >= Double(crit) { return dataCritical }
        if usedPercent >= Double(warn) { return dataWarning }
        return nil
    }
}

/// "91" or "91.5": a fill as alerts name it.
public func storagePercentText(_ percent: Double) -> String {
    let tenths = (percent * 10).rounded()
    return tenths.truncatingRemainder(dividingBy: 10) == 0 ? String(Int(tenths / 10)) : String(format: "%.1f", tenths / 10)
}

/// A storage issue's stored value, "severity|kind|hostname|name|percent|free|size|detail", split
/// up: what the notification names, kept so a resolved one can still be named once it is gone.
public struct StorageIssue: Equatable, Sendable {
    public static let fill = "fill"
    public static let smart = "smart"

    /// dataCritical or dataWarning.
    public let severity: String
    /// `fill` (a volume over a threshold) or `smart` (a disk failing SMART).
    public let kind: String
    public let hostname: String
    /// The volume's name, or the disk's device.
    public let name: String
    /// The fill as storagePercentText wrote it ("" for a disk).
    public let percent: String
    public let freeBytes: UInt64
    public let sizeBytes: UInt64
    /// Why a disk fails SMART, else its model.
    public let detail: String

    public init(severity: String, kind: String, hostname: String, name: String, percent: String = "",
                freeBytes: UInt64 = 0, sizeBytes: UInt64 = 0, detail: String = "") {
        self.severity = severity
        self.kind = kind
        self.hostname = hostname
        self.name = name
        self.percent = percent
        self.freeBytes = freeBytes
        self.sizeBytes = sizeBytes
        self.detail = detail
    }

    public init(value: String) {
        let parts = value.split(separator: "|", maxSplits: 7, omittingEmptySubsequences: false).map(String.init)
        func part(_ i: Int) -> String { parts.count > i ? parts[i] : "" }
        severity = part(0).isEmpty ? dataWarning : part(0)
        kind = part(1).isEmpty ? Self.fill : part(1)
        hostname = part(2)
        name = part(3)
        percent = part(4)
        freeBytes = UInt64(part(5)) ?? 0
        sizeBytes = UInt64(part(6)) ?? 0
        detail = part(7)
    }

    public var isSmart: Bool { kind == Self.smart }

    /// The stored form; the fields but the last lose any "|" so the parts still split.
    public var value: String {
        let safe = { (s: String) in s.replacingOccurrences(of: "|", with: "/") }
        return [severity, kind, safe(hostname), safe(name), safe(percent), String(freeBytes), String(sizeBytes), detail]
            .joined(separator: "|")
    }
}

/// The issues worth a notification, keyed as the Go core keys them ("<node>|<volume>",
/// "<node>|smart|<device>"): a volume at or over `thresholds`, a disk failing SMART (always
/// critical). A disk without SMART data or a healthy one is no issue. A node that did not answer
/// keeps its issues `known` (the previous snapshot's): unreadable, not resolved.
public func storageIssuesOf(_ health: ClusterStorageHealth, thresholds: StorageThresholds,
                            known: [String: String] = [:]) -> [String: String] {
    var out: [String: String] = [:]
    for node in health.nodes {
        let hostname = node.hostname.isEmpty ? node.node : node.hostname
        if let error = node.error, !error.isEmpty {
            let prefix = "\(node.node)|"
            for (key, value) in known where key.hasPrefix(prefix) { out[key] = value }
            continue
        }
        for volume in node.volumes where !volume.key.isEmpty {
            guard let severity = thresholds.severity(usedPercent: volume.usedPercent) else { continue }
            out[volume.key] = StorageIssue(severity: severity, kind: StorageIssue.fill, hostname: hostname, name: volume.name,
                                           percent: storagePercentText(volume.usedPercent),
                                           freeBytes: volume.freeBytes, sizeBytes: volume.sizeBytes).value
        }
        for disk in node.disks where disk.health == "failing" && !disk.key.isEmpty {
            out[disk.key] = StorageIssue(severity: dataCritical, kind: StorageIssue.smart, hostname: hostname, name: disk.device,
                                         detail: String((disk.reason.isEmpty ? disk.model : disk.reason).prefix(200))).value
        }
    }
    return out
}

/// The storage issues a node that did not answer may carry over: the previous snapshot's, only
/// when it is the same cluster and that check watched and read them.
public func knownStorageIssues(_ previous: ClusterSnapshot?, context: String) -> [String: String] {
    guard let previous, previous.context == context, previous.storageWatched, previous.storageChecked else { return [:] }
    return previous.storageIssues
}

/// The node address of a storage issue key: what comes before its first "|".
public func storageIssueNode(_ key: String) -> String {
    String(key.split(separator: "|", maxSplits: 1, omittingEmptySubsequences: false).first ?? "")
}

/// English title of a storage issue.
func storageProblemTitle(_ issue: StorageIssue) -> String {
    if issue.isSmart { return "SMART failing on \(issue.name) (\(issue.hostname))" }
    let title = "\(issue.name) on \(issue.hostname) at \(issue.percent) %"
    return issue.severity == dataCritical ? "\(title), almost full" : title
}

/// English text of a storage issue: the free space, or why the disk fails.
func storageProblemText(_ issue: StorageIssue) -> String {
    issue.isSmart ? issue.detail : "\(formatBytes(issue.freeBytes)) free of \(formatBytes(issue.sizeBytes))"
}

/// English title of a storage issue resolved, `warn` the warning threshold of that check.
func storageClearedTitle(_ issue: StorageIssue, warn: Int) -> String {
    issue.isSmart ? "\(issue.name) on \(issue.hostname) passes SMART again" : "\(issue.name) on \(issue.hostname) back under \(warn) %"
}

/// Node storage issues (see evaluateTrack), keyed "storage:<key>". A resolved one is named from
/// what the previous snapshot kept of it.
func evaluateStorage(previous: ClusterSnapshot?, current: ClusterSnapshot, comparable: Bool, alerts: inout [Alert]) -> IssueTrack {
    let known = previous?.storageIssues ?? [:]
    return evaluateTrack(
        previous: previous?.storageTrack, current: current.storageTrack, comparable: comparable,
        severity: { StorageIssue(value: $0).severity },
        problem: { key, value in
            let issue = StorageIssue(value: value)
            return Alert(key: "storage:\(key)", title: storageProblemTitle(issue), text: storageProblemText(issue), problem: true)
        },
        cleared: { key in
            let issue = StorageIssue(value: known[key] ?? "")
            return Alert(key: "storage:\(key)", title: storageClearedTitle(issue, warn: current.storageWarn), text: "", problem: false)
        },
        alerts: &alerts)
}

import Foundation

public struct NetworkCounters: Codable, Equatable, Sendable {
    public let name: String
    public let rx, tx, errors, drops: UInt64
}
public struct DiskCounters: Codable, Equatable, Sendable {
    public let name: String
    public let read, write, operations, timeMs, busyMs: UInt64
}
public struct DeviceRate: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    public let read, write, errors, drops, busy, latency: Double
    public var id: String { name }
}
public struct Bottlenecks: Decodable, Equatable, Sendable {
    public let wait, steal: Double
    public let network, disks: [DeviceRate]
    public let errors: [String: String]
}
public struct DriftNode: Decodable, Equatable, Identifiable, Sendable {
    public let node, hostname, role: String
    public let values, errors: [String: String]
    public var id: String { node }
}
public struct DriftSnapshot: Decodable, Equatable, Sendable {
    public let scope: String
    public let at: Int64
    public let nodes: [DriftNode]
}
/// One item of a list-valued setting whose value differs between the node and its reference.
public struct DriftPair: Codable, Equatable, Hashable, Sendable {
    public let name, before, after: String
    public init(name: String, before: String, after: String) {
        self.name = name
        self.before = before
        self.after = after
    }
}
/// A setting that differs from the reference. `added`, `removed` and `changed` itemise a
/// list-valued setting; they are empty for scalar keys and from cores older than the itemised
/// diff, which then show `before` → `after` only.
public struct DriftChange: Codable, Equatable, Identifiable, Sendable {
    public let node, reference, key, before, after: String
    public let nodeName, referenceName: String
    public let added, removed: [String]
    public let changed: [DriftPair]
    public var id: String { "\(node)/\(key)" }

    public init(
        node: String, reference: String, key: String, before: String, after: String,
        nodeName: String = "", referenceName: String = "",
        added: [String] = [], removed: [String] = [], changed: [DriftPair] = []
    ) {
        self.node = node
        self.reference = reference
        self.key = key
        self.before = before
        self.after = after
        self.nodeName = nodeName
        self.referenceName = referenceName
        self.added = added
        self.removed = removed
        self.changed = changed
    }

    private enum CodingKeys: String, CodingKey {
        case node, reference, key, before, after, nodeName, referenceName, added, removed, changed
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.decode(String.self, forKey: .node)
        reference = try c.decode(String.self, forKey: .reference)
        key = try c.decode(String.self, forKey: .key)
        before = try c.decodeIfPresent(String.self, forKey: .before) ?? ""
        after = try c.decodeIfPresent(String.self, forKey: .after) ?? ""
        nodeName = try c.decodeIfPresent(String.self, forKey: .nodeName) ?? ""
        referenceName = try c.decodeIfPresent(String.self, forKey: .referenceName) ?? ""
        // Go writes a nil slice as null.
        added = try c.decodeIfPresent([String].self, forKey: .added) ?? []
        removed = try c.decodeIfPresent([String].self, forKey: .removed) ?? []
        changed = try c.decodeIfPresent([DriftPair].self, forKey: .changed) ?? []
    }

    /// Number of itemised differences; 0 for a scalar key or an older core.
    public var itemCount: Int { added.count + removed.count + changed.count }
    /// True when the change is itemised; otherwise show the single before → after line.
    public var isItemised: Bool { itemCount > 0 }
}

/// Shortens a long value (a schematic hash) for a diff row: over 16 characters it keeps the
/// first 8 and an ellipsis. The full value stays available to copy.
public func shortDriftValue(_ value: String) -> String {
    value.count > 16 ? String(value.prefix(8)) + "…" : value
}
public struct IncidentEntry: Decodable, Equatable, Identifiable, Sendable {
    public let id: String
    public let at: Int64
    public let node, kind, subject, detail, severity: String
    public let metrics: Bottlenecks?
    public let metricsOmitted: Int?
}
public struct IncidentDocument: Decodable, Equatable, Sendable {
    public let scope: String
    public let startedAt, updatedAt: Int64
    public let dropped: Int
    public let entries: [IncidentEntry]
}

public extension IncidentEntry {
    /// Old raw details may have been truncated. Do not invent facts from invalid JSON.
    var evidenceDetail: [String: Any]? {
        (try? JSONSerialization.jsonObject(with: Data(detail.utf8))) as? [String: Any]
    }
    var evidenceMetrics: Bottlenecks? {
        if let metrics { return metrics }
        guard let rates = evidenceDetail?["rates"],
            let data = try? JSONSerialization.data(withJSONObject: rates)
        else { return nil }
        return try? JSONDecoder().decode(Bottlenecks.self, from: data)
    }
}

import Foundation

/// A node's clock compared with its NTP server (NodeTime / ClusterTime).
public struct NodeTimeInfo: Decodable, Equatable, Identifiable, Sendable {
    public let node: String
    public let server: String
    /// Unix ms, node clock.
    public let localTime: Int64
    /// Unix ms, NTP server clock.
    public let remoteTime: Int64
    /// remoteTime - localTime: positive when the node is behind.
    public let offsetMs: Int64
    public let error: String?

    public var id: String { node }

    public init(node: String, server: String = "", localTime: Int64 = 0, remoteTime: Int64 = 0, offsetMs: Int64 = 0, error: String? = nil) {
        self.node = node
        self.server = server
        self.localTime = localTime
        self.remoteTime = remoteTime
        self.offsetMs = offsetMs
        self.error = error
    }

    private enum CodingKeys: String, CodingKey { case node, server, localTime, remoteTime, offsetMs, error }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.decode(String.self, forKey: .node)
        server = try c.field(.server, "")
        localTime = try c.field(.localTime, 0)
        remoteTime = try c.field(.remoteTime, 0)
        offsetMs = try c.field(.offsetMs, 0)
        let message = try c.decodeIfPresent(String.self, forKey: .error)
        error = message?.isEmpty == true ? nil : message
    }

    public var drift: TimeDrift { timeDrift(offsetMs: offsetMs, failed: error != nil) }
}

public struct ClusterTimeInfo: Decodable, Equatable, Sendable {
    public let context: String
    public let nodes: [NodeTimeInfo]

    public init(context: String, nodes: [NodeTimeInfo]) {
        self.context = context
        self.nodes = nodes
    }

    private enum CodingKeys: String, CodingKey { case context, nodes }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        context = try c.field(.context, "")
        nodes = try c.field(.nodes, [])
    }

    /// The worst drift in the cluster, unreachable nodes counting as bad (ok for no nodes).
    public var worst: TimeDrift { nodes.map(\.drift).max() ?? .ok }

    /// The overview badge and summary line: drift of the nodes that answered only.
    public var summary: TimeDriftSummary { TimeDriftSummary(nodes) }
}

/// Clock drift of the reachable nodes, with the unreachable ones counted apart so a node
/// that does not answer is not reported as out of sync (same rules as Android).
public struct TimeDriftSummary: Equatable, Sendable {
    public let reachable: Int
    public let unreachable: Int
    /// Reachable nodes at warning or worse.
    public let drifting: Int
    /// Worst drift of the reachable nodes; nil when none answered.
    public let status: TimeDrift?
    /// Largest |offset| among reachable nodes.
    public let maxOffsetMs: Int64

    public init(_ nodes: [NodeTimeInfo]) {
        let answered = nodes.filter { $0.error == nil }
        reachable = answered.count
        unreachable = nodes.count - answered.count
        drifting = answered.filter { $0.drift != .ok }.count
        status = answered.map(\.drift).max()
        maxOffsetMs = answered.map { $0.offsetMs == .min ? .max : abs($0.offsetMs) }.max() ?? 0
    }

    /// Collapsed to one line unless a reachable node drifts (same as Android).
    public var expandedByDefault: Bool { (status ?? .ok) != .ok }
}

/// Clock drift severity; same thresholds as Android.
public enum TimeDrift: Int, Comparable, Sendable {
    case ok, warning, bad

    public static func < (a: TimeDrift, b: TimeDrift) -> Bool { a.rawValue < b.rawValue }
}

/// |offset| ≥ 500 ms is a warning (etcd and TLS start to suffer); ≥ 5 s, or no answer, is bad.
public let timeDriftWarningMs: Int64 = 500
public let timeDriftBadMs: Int64 = 5_000

public func timeDrift(offsetMs: Int64, failed: Bool = false) -> TimeDrift {
    if failed { return .bad }
    let magnitude = offsetMs.magnitude
    if magnitude >= UInt64(timeDriftBadMs) { return .bad }
    if magnitude >= UInt64(timeDriftWarningMs) { return .warning }
    return .ok
}

/// "12 ms", "1.25 s": formatOffset without the sign, for "max ±12 ms".
public func formatOffsetMagnitude(_ ms: Int64) -> String {
    String(formatOffset(ms == .min ? .max : abs(ms)).dropFirst())
}

/// "+12 ms", "-1.25 s", "+2 min 5 s": signed like the Go offset (positive = node behind).
public func formatOffset(_ ms: Int64) -> String {
    let sign = ms < 0 ? "-" : "+"
    let magnitude = ms.magnitude
    if magnitude < 1_000 { return "\(sign)\(magnitude) ms" }
    if magnitude < 60_000 { return sign + String(format: "%.2f s", Double(magnitude) / 1_000) }
    let seconds = magnitude / 1_000
    return "\(sign)\(seconds / 60) min \(seconds % 60) s"
}

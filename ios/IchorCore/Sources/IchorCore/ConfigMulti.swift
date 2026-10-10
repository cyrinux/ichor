import Foundation

// Mirrors go/ichorgo/configmulti.go (MachineConfigMultiPreview, StartConfigApplyMulti).

/// One node's preview of the same edits: its own diff, or why the edits do not fit it (`error`).
public struct MultiConfigNodePreview: Decodable, Equatable, Identifiable, Sendable {
    public let node: String
    public let hostname: String
    public let changed: Bool
    public let lines: [ConfigDiffLine]
    public let needsReboot: Bool
    public let error: String?

    public var id: String { node }
    public var name: String { hostname.isEmpty ? node : hostname }

    private enum CodingKeys: String, CodingKey { case node, hostname, changed, lines, needsReboot, error }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.decode(String.self, forKey: .node)
        hostname = try c.field(.hostname, "")
        changed = try c.field(.changed, false)
        lines = (try c.field(.lines, [ConfigDiffLine]())).enumerated().map {
            ConfigDiffLine(id: $0.offset, kind: $0.element.kind, text: $0.element.text)
        }
        needsReboot = try c.field(.needsReboot, false)
        error = try c.decodeIfPresent(String.self, forKey: .error)
    }
}

public struct MultiConfigPreview: Decodable, Equatable, Sendable {
    public let nodes: [MultiConfigNodePreview]
    public let anyReboot: Bool

    /// The nodes the run would change: the others are skipped or already have the change.
    public var changing: [MultiConfigNodePreview] { nodes.filter { $0.error == nil && $0.changed } }

    /// No try (one node only); no "now without a reboot" when one node would need one.
    public var applyModes: [ConfigApplyMode] { anyReboot ? [.staged, .reboot] : ConfigApplyMode.allCases }

    private enum CodingKeys: String, CodingKey { case nodes, anyReboot }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        nodes = try c.field(.nodes, [])
        anyReboot = try c.field(.anyReboot, false)
    }
}

/// A node's line in a multi-node apply.
public struct MultiConfigNodeState: Decodable, Equatable, Identifiable, Sendable {
    public enum State: String, Sendable {
        case pending, applying, done, skipped, unchanged, failed
    }

    public let node: String
    public let hostname: String
    /// Go's state; an unknown one reads as pending.
    public let state: String
    public let error: String?

    public var id: String { node }
    public var name: String { hostname.isEmpty ? node : hostname }
    public var nodeState: State { State(rawValue: state) ?? .pending }

    private enum CodingKeys: String, CodingKey { case node, hostname, state, error }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.decode(String.self, forKey: .node)
        hostname = try c.field(.hostname, "")
        state = try c.field(.state, "pending")
        error = try c.decodeIfPresent(String.self, forKey: .error)
    }
}

/// A multi-node apply's progress: the phase of the node at `index`, and every node's state.
public struct MultiConfigProgress: Decodable, Equatable, Sendable {
    public let phase: String
    public let message: String
    public let index: Int
    public let total: Int
    public let node: String
    public let nodes: [MultiConfigNodeState]

    private enum CodingKeys: String, CodingKey { case phase, message, index, total, node, nodes }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        phase = try c.field(.phase, "")
        message = try c.field(.message, "")
        index = try c.field(.index, 0)
        total = try c.field(.total, 0)
        node = try c.field(.node, "")
        nodes = try c.field(.nodes, [])
    }
}

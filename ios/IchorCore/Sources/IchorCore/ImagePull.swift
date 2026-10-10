import Foundation

/// Where an image is pulled (Go StartImagePull): Talos installer and system images, or
/// Kubernetes (CRI) images.
public enum ImagePullNamespace: String, CaseIterable, Sendable {
    case system, cri
}

/// One node's state in an image pull.
public enum ImagePullState: String, Sendable {
    case pending, pulling, done, failed
}

public struct ImagePullNode: Decodable, Equatable, Sendable, Identifiable {
    public let node: String
    public let hostname: String
    public let state: ImagePullState
    public let error: String

    public var id: String { node }
    public var label: String { hostname.isEmpty ? node : hostname }

    public init(node: String, hostname: String = "", state: ImagePullState = .pending, error: String = "") {
        self.node = node
        self.hostname = hostname
        self.state = state
        self.error = error
    }

    private enum CodingKeys: String, CodingKey { case node, hostname, state, error }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.field(.node, "")
        hostname = try c.field(.hostname, "")
        state = ImagePullState(rawValue: try c.field(.state, "")) ?? .pending
        error = try c.field(.error, "")
    }
}

/// One OnProgress event of an image pull: every node's state; `at` is unix ms.
public struct ImagePullProgress: Decodable, Equatable, Sendable {
    public let nodes: [ImagePullNode]
    public let done: Int
    public let total: Int
    public let at: Int64

    public var failed: Int { nodes.filter { $0.state == .failed }.count }

    public init(nodes: [ImagePullNode] = [], done: Int = 0, total: Int = 0, at: Int64 = 0) {
        self.nodes = nodes
        self.done = done
        self.total = total
        self.at = at
    }

    private enum CodingKeys: String, CodingKey { case nodes, done, total, at }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        nodes = try c.field(.nodes, [])
        done = try c.field(.done, 0)
        total = try c.field(.total, 0)
        at = try c.field(.at, 0)
    }
}

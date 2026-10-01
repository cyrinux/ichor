import Foundation

// Mirrors the JSON produced by the Go core (go/talosmobile), like the Android models.

public struct ConfigSummary: Decodable, Equatable, Sendable {
    public let current: String
    public let contexts: [ContextSummary]

    public func context(named name: String) -> ContextSummary? {
        contexts.first { $0.name == name }
    }
}

public struct ContextSummary: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    public let endpoints: [String]
    public let nodes: [String]
    public let roles: [String]
    public let certNotAfter: Int64

    public var id: String { name }

    public init(name: String, endpoints: [String] = [], nodes: [String] = [], roles: [String] = [], certNotAfter: Int64 = 0) {
        self.name = name
        self.endpoints = endpoints
        self.nodes = nodes
        self.roles = roles
        self.certNotAfter = certNotAfter
    }

    private enum CodingKeys: String, CodingKey { case name, endpoints, nodes, roles, certNotAfter }

    // Go encodes empty (nil) slices as null here.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.decode(String.self, forKey: .name)
        endpoints = try c.decodeIfPresent([String].self, forKey: .endpoints) ?? []
        nodes = try c.decodeIfPresent([String].self, forKey: .nodes) ?? []
        roles = try c.decodeIfPresent([String].self, forKey: .roles) ?? []
        certNotAfter = try c.decodeIfPresent(Int64.self, forKey: .certNotAfter) ?? 0
    }
}

public struct ClusterOverview: Decodable, Equatable, Sendable {
    public let context: String
    public let nodes: [NodeOverview]
}

public enum NodeHealth: String, Sendable, CaseIterable {
    case ready, notReady, unreachable
}

public struct NodeOverview: Decodable, Equatable, Identifiable, Hashable, Sendable {
    public let node: String
    public let hostname: String
    public let reachable: Bool
    public let error: String?
    public let version: String
    public let arch: String
    public let platform: String
    public let role: String
    public let stage: String
    public let ready: Bool
    public let unmetConditions: [UnmetCondition]

    public var id: String { node }

    public var health: NodeHealth {
        if !reachable { return .unreachable }
        return ready ? .ready : .notReady
    }
}

public struct UnmetCondition: Decodable, Equatable, Hashable, Sendable {
    public let name: String
    public let reason: String
}

public struct ServiceInfo: Decodable, Equatable, Identifiable, Sendable {
    public let id: String
    public let state: String
    public let health: String // healthy | unhealthy | unknown
    public let message: String?
    public let lastEvent: String?
    public let lastChange: Int64?
}

public struct NodeResources: Decodable, Equatable, Sendable {
    public let memTotal: UInt64
    public let memAvailable: UInt64
    public let load1: Double
    public let load5: Double
    public let load15: Double
    public let bootTime: UInt64
    public let cpuCount: Int
    public let cpuModel: String
    public let mounts: [MountUsage]
}

public struct MountUsage: Decodable, Equatable, Identifiable, Sendable {
    public let filesystem: String
    public let mountedOn: String
    public let size: UInt64
    public let available: UInt64

    public var id: String { mountedOn }
}

public struct EtcdOverview: Decodable, Equatable, Sendable {
    public let error: String?
    public let leaderId: String
    public let members: [EtcdMember]
    public let statuses: [EtcdNodeStatus]
    public let alarms: [EtcdAlarm]
}

public struct EtcdMember: Decodable, Equatable, Identifiable, Sendable {
    public let id: String
    public let hostname: String
    public let peerUrls: [String]
    public let clientUrls: [String]
    public let isLearner: Bool
}

public struct EtcdNodeStatus: Decodable, Equatable, Identifiable, Sendable {
    public let node: String
    public let error: String?
    public let memberId: String
    public let isLeader: Bool
    public let isLearner: Bool
    public let dbSize: Int64
    public let dbSizeInUse: Int64
    public let raftIndex: UInt64
    public let raftTerm: UInt64
    public let version: String
    public let errors: [String]

    public var id: String { node }
}

public struct EtcdAlarm: Decodable, Equatable, Hashable, Sendable {
    public let memberId: String
    public let alarm: String
}

public struct LogTail: Decodable, Equatable, Sendable {
    public let lines: [String]
    public let truncated: Bool
}

/// Features gated by Talos RBAC (rules from Talos v1.14 machined.go).
public enum Feature: CaseIterable, Sendable {
    case power, health, kubeconfig, debugShell

    public var label: String {
        switch self {
        case .power: "Reboot / shutdown"
        case .health: "Cluster health check"
        case .kubeconfig: "Kubeconfig export"
        case .debugShell: "Debug shell"
        }
    }

    public var roles: Set<String> {
        switch self {
        case .power: ["os:admin", "os:operator"]
        // The server-side health check fetches a Kubernetes admin kubeconfig with the caller's role.
        // DebugService/ContainerRun is admin-only too.
        case .health, .kubeconfig, .debugShell: ["os:admin"]
        }
    }

    public var minimumRole: String { roles.contains("os:operator") ? "os:operator" : "os:admin" }
}

public extension ContextSummary {
    func allows(_ feature: Feature) -> Bool { roles.contains { feature.roles.contains($0) } }
}

public enum TalosJSON {
    public static func decode<T: Decodable>(_ type: T.Type, from json: String) throws -> T {
        try JSONDecoder().decode(type, from: Data(json.utf8))
    }
}

public struct KubeSpanOverview: Decodable, Equatable, Sendable {
    public let nodes: [KubeSpanNode]
}

public struct KubeSpanNode: Decodable, Equatable, Identifiable, Sendable {
    public let node: String
    public let error: String?
    public let enabled: Bool
    public let up: Int
    public let down: Int
    public let peers: [KubeSpanPeer]

    public var id: String { node }
}

public struct KubeSpanPeer: Decodable, Equatable, Identifiable, Sendable {
    public let publicKey: String
    public let label: String
    public let state: String // up | down | unknown
    public let endpoint: String
    public let rx: Int64
    public let tx: Int64
    public let lastHandshake: Int64

    public var id: String { publicKey }
}

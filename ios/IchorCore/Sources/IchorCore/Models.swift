import Foundation

// Mirrors the JSON produced by the Go core (go/talosmobile), like the Android models.

public struct ConfigSummary: Decodable, Equatable, Sendable {
    public let current: String
    public let contexts: [ContextSummary]

    public init(current: String, contexts: [ContextSummary]) {
        self.current = current
        self.contexts = contexts
    }

    public func context(named name: String) -> ContextSummary? {
        contexts.first { $0.name == name }
    }

    /// The context to open: the saved position first (names differ while the screenshot mode
    /// masks them; Go keeps the order), then the saved name (installs from before the
    /// position was saved), then the config's current context. Same rule as Android.
    public func selectedContext(index: Int?, name: String?) -> String {
        if let index, contexts.indices.contains(index) { return contexts[index].name }
        return name.flatMap { context(named: $0)?.name } ?? current
    }

    /// The context `step` positions after `active` (before it when negative), nil past
    /// either end. Same rule as Android.
    public func adjacentContext(to active: String, step: Int) -> String? {
        guard step != 0, let index = contexts.firstIndex(where: { $0.name == active }),
              contexts.indices.contains(index + step) else { return nil }
        return contexts[index + step].name
    }

    /// Where the active context is once the one at `removed` is gone, among the `remaining`
    /// ones: it keeps showing the same context, or the removed one's neighbour.
    public static func activeIndexAfterRemoval(active: Int, removed: Int, remaining: Int) -> Int {
        if active > removed { return active - 1 }
        if active == removed { return min(removed, remaining - 1) }
        return active
    }
}

public struct ContextSummary: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    /// Identifies the cluster whatever the screenshot mode does to `name`; keys its color.
    public let fingerprint: String
    public let endpoints: [String]
    public let nodes: [String]
    public let roles: [String]
    public let certNotAfter: Int64
    public let demo: Bool

    public var id: String { name }

    public init(name: String, fingerprint: String = "", endpoints: [String] = [], nodes: [String] = [], roles: [String] = [], certNotAfter: Int64 = 0, demo: Bool = false) {
        self.name = name
        self.fingerprint = fingerprint
        self.endpoints = endpoints
        self.nodes = nodes
        self.roles = roles
        self.certNotAfter = certNotAfter
        self.demo = demo
    }

    private enum CodingKeys: String, CodingKey { case name, fingerprint, endpoints, nodes, roles, certNotAfter, demo }

    // Go encodes empty (nil) slices as null here.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.decode(String.self, forKey: .name)
        fingerprint = try c.decodeIfPresent(String.self, forKey: .fingerprint) ?? ""
        endpoints = try c.decodeIfPresent([String].self, forKey: .endpoints) ?? []
        nodes = try c.decodeIfPresent([String].self, forKey: .nodes) ?? []
        roles = try c.decodeIfPresent([String].self, forKey: .roles) ?? []
        certNotAfter = try c.decodeIfPresent(Int64.self, forKey: .certNotAfter) ?? 0
        demo = try c.decodeIfPresent(Bool.self, forKey: .demo) ?? false
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
    /// Parsed `lines` (same order and count); nil from an older core or when they do not
    /// decode, see logEntries.
    public let entries: [LogEntry]?

    private enum CodingKeys: String, CodingKey { case lines, truncated, entries }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        lines = try c.decodeIfPresent([String].self, forKey: .lines) ?? []
        truncated = try c.decodeIfPresent(Bool.self, forKey: .truncated) ?? false
        entries = try? c.decodeIfPresent([LogEntry].self, forKey: .entries)
    }
}

/// Features gated by Talos RBAC (rules from Talos v1.14 machined.go).
public enum Feature: CaseIterable, Sendable {
    case power, health, kubeconfig, debugShell, etcdDefrag, machineConfig, etcdSnapshot, serviceControl, issueConfig, packetCapture, upgrade
    case etcdMemberActions, resourceBrowser, supportBundle

    public var label: String {
        switch self {
        case .power: "Reboot / shutdown"
        case .health: "Cluster health check"
        case .kubeconfig: "Kubeconfig export"
        case .debugShell: "Debug shell"
        case .etcdDefrag: "etcd defragmentation"
        case .machineConfig: "Machine config"
        case .etcdSnapshot: "etcd snapshot"
        case .serviceControl: "Service control"
        case .issueConfig: "Issue talosconfig"
        case .packetCapture: "Packet capture"
        case .upgrade: "Talos upgrade"
        case .etcdMemberActions: "etcd member actions"
        case .resourceBrowser: "Resources browser"
        case .supportBundle: "Support bundle"
        }
    }

    public var roles: Set<String> {
        switch self {
        case .power, .etcdDefrag, .serviceControl, .packetCapture: ["os:admin", "os:operator"]
        // The server-side health check fetches a Kubernetes admin kubeconfig with the caller's role.
        // DebugService/ContainerRun is admin-only too.
        // The machine config holds the cluster secrets (CA keys, tokens).
        // Issuing a client certificate (GenerateClientConfiguration) is admin-only.
        // MachineService/Upgrade is admin-only.
        // Forfeiting leadership and removing a member (EtcdForfeitLeadership, EtcdRemoveMemberByID)
        // are admin-only.
        case .health, .kubeconfig, .debugShell, .machineConfig, .issueConfig, .upgrade, .etcdMemberActions: ["os:admin"]
        // Reading resources (COSI state) is open to every role; Talos itself filters what a
        // reader may see of the sensitive ones. A support bundle too: the parts the role cannot
        // read (machine config: os:admin, etcd status: os:operator) are left out and noted in it.
        case .resourceBrowser, .supportBundle: ["os:admin", "os:operator", "os:reader"]
        // A snapshot holds every Kubernetes Secret; Talos has a dedicated role for it.
        case .etcdSnapshot: ["os:admin", "os:operator", "os:etcd:backup"]
        }
    }

    public var minimumRole: String {
        if roles.contains("os:reader") { return "os:reader" }
        return roles.contains("os:operator") ? "os:operator" : "os:admin"
    }
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

public extension EtcdNodeStatus {
    /// Space a defragmentation would give back (on-disk size minus space in use).
    var reclaimable: Int64 { max(dbSize - dbSizeInUse, 0) }
}

/// Members to defragment one at a time, as Talos advises: followers first, the leader last,
/// skipping members that could not be queried (same rule as Android).
public func defragOrder(_ statuses: [EtcdNodeStatus]) -> [EtcdNodeStatus] {
    statuses
        .filter { $0.error == nil && !$0.memberId.isEmpty }
        .sorted { a, b in
            if a.isLeader != b.isLeader { return !a.isLeader }
            return a.reclaimable > b.reclaimable
        }
}

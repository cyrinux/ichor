import Foundation

// Mirrors go/ichorgo/kube_clusterapi.go.

/// A management cluster's Cluster API state (and Sidero Metal's): the workload clusters, their
/// machines and machine deployments, and the bare-metal servers, problems first in every list.
public struct ClusterLifecycle: Decodable, Equatable, Sendable {
    /// The cluster serves cluster.x-k8s.io or metal.sidero.dev.
    public let installed: Bool
    /// The served cluster.x-k8s.io version, "" without Cluster API.
    public let version: String
    public let sidero: Bool
    public let talosControlPlane: Bool
    public let talosBootstrap: Bool
    /// A list that failed; what could be read still shows.
    public let error: String
    /// A kind had more objects than the reader keeps.
    public let truncated: Bool
    public let clusters: [CapiCluster]
    public let machines: [CapiMachine]
    public let machineDeployments: [CapiMachineDeployment]
    public let servers: [SideroServer]
    public let serverClasses: [SideroServerClass]
    public let counts: ClusterLifecycleCounts

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        installed = try c.field(.installed, false)
        version = try c.field(.version, "")
        sidero = try c.field(.sidero, false)
        talosControlPlane = try c.field(.talosControlPlane, false)
        talosBootstrap = try c.field(.talosBootstrap, false)
        error = try c.field(.error, "")
        truncated = try c.field(.truncated, false)
        clusters = try c.field(.clusters, [])
        machines = try c.field(.machines, [])
        machineDeployments = try c.field(.machineDeployments, [])
        servers = try c.field(.servers, [])
        serverClasses = try c.field(.serverClasses, [])
        counts = try c.field(.counts, ClusterLifecycleCounts())
    }

    private enum CodingKeys: String, CodingKey {
        case installed, version, sidero, talosControlPlane, talosBootstrap, error, truncated
        case clusters, machines, machineDeployments, servers, serverClasses, counts
    }

    /// Nothing to list.
    public var isEmpty: Bool {
        clusters.isEmpty && machines.isEmpty && machineDeployments.isEmpty && servers.isEmpty && serverClasses.isEmpty
    }

    /// The API version the object summary reads for `kind`: the served Cluster API one (the
    /// reader's default when it named none), Sidero Metal's fixed one.
    public func apiVersion(_ kind: LifecycleKind) -> String {
        kind.group == LifecycleKind.capiGroup ? (version.isEmpty ? LifecycleKind.capiDefaultVersion : version) : LifecycleKind.sideroVersion
    }

    /// The machines by cluster (its namespace too: two clusters may share a name), the clusters
    /// in the order their first machine comes (problems first, as the core sorted them), each
    /// cluster's control plane first. `id` is "namespace/cluster".
    public var machinesByCluster: [(id: String, cluster: String, machines: [CapiMachine])] {
        var order: [String] = []
        var groups: [String: [CapiMachine]] = [:]
        for m in machines {
            let key = "\(m.namespace)/\(m.cluster)"
            if groups[key] == nil { order.append(key) }
            groups[key, default: []].append(m)
        }
        return order.map { key in
            let list = groups[key] ?? []
            return (key, list.first?.cluster ?? "", list.filter(\.controlPlane) + list.filter { !$0.controlPlane })
        }
    }
}

public struct ClusterLifecycleCounts: Decodable, Equatable, Sendable {
    public let clusters: Int
    public let machines: Int
    public let servers: Int
    public let ok: Int
    public let warning: Int
    public let critical: Int

    public init(clusters: Int = 0, machines: Int = 0, servers: Int = 0, ok: Int = 0, warning: Int = 0, critical: Int = 0) {
        self.clusters = clusters
        self.machines = machines
        self.servers = servers
        self.ok = ok
        self.warning = warning
        self.critical = critical
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        clusters = try c.field(.clusters, 0)
        machines = try c.field(.machines, 0)
        servers = try c.field(.servers, 0)
        ok = try c.field(.ok, 0)
        warning = try c.field(.warning, 0)
        critical = try c.field(.critical, 0)
    }

    private enum CodingKeys: String, CodingKey { case clusters, machines, servers, ok, warning, critical }
}

public struct CapiReplicas: Decodable, Equatable, Sendable {
    public let desired: Int
    public let ready: Int
    public let available: Int
    public let upToDate: Int

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        desired = try c.field(.desired, 0)
        ready = try c.field(.ready, 0)
        available = try c.field(.available, 0)
        upToDate = try c.field(.upToDate, 0)
    }

    private enum CodingKeys: String, CodingKey { case desired, ready, available, upToDate }

    /// "2/4": ready of desired.
    public var ratio: String { "\(ready)/\(desired)" }
}

/// A condition that is not True.
public struct CapiCondition: Decodable, Equatable, Sendable {
    public let type: String
    public let status: String
    public let reason: String
    public let message: String

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        type = try c.field(.type, "")
        status = try c.field(.status, "")
        reason = try c.field(.reason, "")
        message = try c.field(.message, "")
    }

    private enum CodingKeys: String, CodingKey { case type, status, reason, message }
}

/// Why a row needs attention: its failure first, else its conditions that are not True.
func capiProblem(_ failureReason: String, _ failureMessage: String, _ conditions: [CapiCondition]) -> String {
    let failure = [failureReason, failureMessage].filter { !$0.isEmpty }.joined(separator: ": ")
    if !failure.isEmpty { return failure }
    return conditions.map { c in
        [c.type, c.message.isEmpty ? c.reason : c.message].filter { !$0.isEmpty }.joined(separator: ": ")
    }.joined(separator: "; ")
}

/// A workload cluster.
public struct CapiCluster: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    public let phase: String
    public let paused: Bool
    /// host:port, "" when unset.
    public let endpoint: String
    public let infraKind: String
    public let controlPlaneKind: String
    public let infrastructureReady: Bool
    public let controlPlaneReady: Bool
    /// Nil when the control plane provider reports no replicas.
    public let controlPlane: CapiReplicas?
    public let workers: CapiReplicas?
    public let versions: [String]
    public let conditions: [CapiCondition]
    public let failureReason: String
    public let failureMessage: String
    public let health: ServiceHealth
    public let reasons: [String]

    public var id: String { "\(namespace)/\(name)" }
    public var problem: String { capiProblem(failureReason, failureMessage, conditions) }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        phase = try c.field(.phase, "")
        paused = try c.field(.paused, false)
        endpoint = try c.field(.endpoint, "")
        infraKind = try c.field(.infraKind, "")
        controlPlaneKind = try c.field(.controlPlaneKind, "")
        infrastructureReady = try c.field(.infrastructureReady, false)
        controlPlaneReady = try c.field(.controlPlaneReady, false)
        controlPlane = try c.decodeIfPresent(CapiReplicas.self, forKey: .controlPlane)
        workers = try c.decodeIfPresent(CapiReplicas.self, forKey: .workers)
        versions = try c.field(.versions, [])
        conditions = try c.field(.conditions, [])
        failureReason = try c.field(.failureReason, "")
        failureMessage = try c.field(.failureMessage, "")
        health = try c.wire(.health)
        reasons = try c.field(.reasons, [])
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, phase, paused, endpoint, infraKind, controlPlaneKind, infrastructureReady, controlPlaneReady
        case controlPlane, workers, versions, conditions, failureReason, failureMessage, health, reasons
    }
}

/// A Machine: a node of a workload cluster, from request to join.
public struct CapiMachine: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    public let cluster: String
    public let controlPlane: Bool
    public let deployment: String
    public let phase: String
    /// "" while not joined.
    public let node: String
    public let version: String
    public let addresses: [String]
    /// Unix ms, 0 when never.
    public let lastUpdated: Int64
    /// Unix ms.
    public let createdAt: Int64
    /// Not moving for over half an hour in a transient phase.
    public let stuck: Bool
    public let failureReason: String
    public let failureMessage: String
    public let conditions: [CapiCondition]
    public let health: ServiceHealth
    public let reasons: [String]

    public var id: String { "\(namespace)/\(name)" }
    public var problem: String { capiProblem(failureReason, failureMessage, conditions) }
    /// When it last moved, as far as the report says: its last update, else its creation; 0 unknown.
    public var stuckSince: Int64 { lastUpdated > 0 ? lastUpdated : createdAt }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        cluster = try c.field(.cluster, "")
        controlPlane = try c.field(.controlPlane, false)
        deployment = try c.field(.deployment, "")
        phase = try c.field(.phase, "")
        node = try c.field(.node, "")
        version = try c.field(.version, "")
        addresses = try c.field(.addresses, [])
        lastUpdated = try c.field(.lastUpdated, 0)
        createdAt = try c.field(.createdAt, 0)
        stuck = try c.field(.stuck, false)
        failureReason = try c.field(.failureReason, "")
        failureMessage = try c.field(.failureMessage, "")
        conditions = try c.field(.conditions, [])
        health = try c.wire(.health)
        reasons = try c.field(.reasons, [])
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, cluster, controlPlane, deployment, phase, node, version, addresses, lastUpdated, createdAt, stuck
        case failureReason, failureMessage, conditions, health, reasons
    }
}

public struct CapiMachineDeployment: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    public let cluster: String
    public let phase: String
    public let desired: Int
    public let ready: Int
    public let available: Int
    public let version: String
    public let health: ServiceHealth

    public var id: String { "\(namespace)/\(name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        cluster = try c.field(.cluster, "")
        phase = try c.field(.phase, "")
        desired = try c.field(.desired, 0)
        ready = try c.field(.ready, 0)
        available = try c.field(.available, 0)
        version = try c.field(.version, "")
        health = try c.wire(.health)
    }

    private enum CodingKeys: String, CodingKey { case namespace, name, cluster, phase, desired, ready, available, version, health }
}

/// A Sidero Metal bare-metal server.
public struct SideroServer: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    public let hostname: String
    public let accepted: Bool
    public let cordoned: Bool
    public let inUse: Bool
    public let isClean: Bool
    /// on, off or unknown.
    public let power: String
    public let addresses: [String]
    public let serverClasses: [String]
    public let cpu: String
    public let memoryBytes: Int64
    public let health: ServiceHealth

    public var id: String { name }
    /// The hostname when it has one, else the Server's name.
    public var title: String { hostname.isEmpty ? name : hostname }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        hostname = try c.field(.hostname, "")
        accepted = try c.field(.accepted, false)
        cordoned = try c.field(.cordoned, false)
        inUse = try c.field(.inUse, false)
        isClean = try c.field(.isClean, false)
        power = try c.field(.power, "")
        addresses = try c.field(.addresses, [])
        serverClasses = try c.field(.serverClasses, [])
        cpu = try c.field(.cpu, "")
        memoryBytes = try c.field(.memoryBytes, 0)
        health = try c.wire(.health)
    }

    private enum CodingKeys: String, CodingKey {
        case name, hostname, accepted, cordoned, inUse, isClean, power, addresses, serverClasses, cpu, memoryBytes, health
    }
}

/// A Sidero ServerClass and how many of its servers are free or taken.
public struct SideroServerClass: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    public let available: Int
    public let inUse: Int

    public var id: String { name }
    /// Every server of it is taken: the next Machine that asks for it waits.
    public var exhausted: Bool { available == 0 && inUse > 0 }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        available = try c.field(.available, 0)
        inUse = try c.field(.inUse, 0)
    }

    private enum CodingKeys: String, CodingKey { case name, available, inUse }
}

/// The kinds the screen lists, with what the object summary reads.
public enum LifecycleKind: Sendable {
    case cluster, machine, machineDeployment, server, serverClass

    public static let capiGroup = "cluster.x-k8s.io"
    public static let sideroGroup = "metal.sidero.dev"
    public static let capiDefaultVersion = "v1beta1"
    public static let sideroVersion = "v1alpha2"

    public var group: String {
        switch self {
        case .cluster, .machine, .machineDeployment: Self.capiGroup
        case .server, .serverClass: Self.sideroGroup
        }
    }

    public var resource: String {
        switch self {
        case .cluster: "clusters"
        case .machine: "machines"
        case .machineDeployment: "machinedeployments"
        case .server: "servers"
        case .serverClass: "serverclasses"
        }
    }

    public var kind: String {
        switch self {
        case .cluster: "Cluster"
        case .machine: "Machine"
        case .machineDeployment: "MachineDeployment"
        case .server: "Server"
        case .serverClass: "ServerClass"
        }
    }

    /// Cluster API's kinds live in a namespace, Sidero Metal's do not.
    public var namespaced: Bool { group == Self.capiGroup }
}

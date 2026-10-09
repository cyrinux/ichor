import Foundation

// Data services: Garage instances and nodes (see DataServices.swift).

public struct GarageStatus: Decodable, Equatable, Sendable {
    public let error: String
    public let instances: [GarageInstance]

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        error = try c.field(.error, "")
        instances = try c.field(.instances, [])
    }

    private enum CodingKeys: String, CodingKey { case error, instances }
}

public struct GarageInstance: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    /// Pod the CLI ran in, "" when none was ready.
    public let pod: String
    public let pods: Int
    public let podsReady: Int
    public let version: String
    public let state: GarageState
    /// English, causes first (from the Go core).
    public let message: String
    public let connectedNodes: Int
    public let knownNodes: Int
    public let storageNodes: Int
    public let storageNodesUp: Int
    public let partitions: Int
    public let partitionsQuorum: Int
    public let partitionsAllOk: Int
    /// -1 when unknown.
    public let resyncQueue: Int64
    public let resyncErrors: Int64
    public let tableSyncQueue: Int64
    public let layoutVersion: Int64
    public let nodes: [GarageNode]
    /// cli-json (full picture) or health (status only).
    public let source: String

    public var id: String { label }
    public var label: String { "\(namespace)/\(name)" }
    public var detailed: Bool { source == "cli-json" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        pod = try c.field(.pod, "")
        pods = try c.field(.pods, 0)
        podsReady = try c.field(.podsReady, 0)
        version = try c.field(.version, "")
        state = GarageState(rawValue: try c.field(.status, "")) ?? .unknown
        message = try c.field(.message, "")
        connectedNodes = try c.field(.connectedNodes, 0)
        knownNodes = try c.field(.knownNodes, 0)
        storageNodes = try c.field(.storageNodes, 0)
        storageNodesUp = try c.field(.storageNodesUp, 0)
        partitions = try c.field(.partitions, 0)
        partitionsQuorum = try c.field(.partitionsQuorum, 0)
        partitionsAllOk = try c.field(.partitionsAllOk, 0)
        resyncQueue = try c.field(.resyncQueue, -1)
        resyncErrors = try c.field(.resyncErrors, -1)
        tableSyncQueue = try c.field(.tableSyncQueue, -1)
        layoutVersion = try c.field(.layoutVersion, 0)
        nodes = try c.field(.nodes, [])
        source = try c.field(.source, "")
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, pod, pods, podsReady, version, status, message, connectedNodes, knownNodes, storageNodes
        case storageNodesUp, partitions, partitionsQuorum, partitionsAllOk, resyncQueue, resyncErrors, tableSyncQueue
        case layoutVersion, nodes, source
    }
}

public struct GarageNode: Decodable, Equatable, Identifiable, Sendable {
    public let nodeID: String
    public let hostname: String
    public let zone: String
    public let tags: [String]
    /// Kubernetes node of the pod with that hostname, "" when none.
    public let kubeNode: String
    /// Holds a role in the current layout: only those count as down.
    public let storage: Bool
    public let up: Bool
    /// -1 when up or unknown.
    public let lastSeenSecs: Int64
    public let draining: Bool
    public let dataAvail: Int64
    public let dataTotal: Int64
    public let resyncQueue: Int64
    public let resyncErrors: Int64
    public let tableSyncQueue: Int64
    public let statsError: String
    /// Resync tranquility: 0 resyncs at full speed, 2 is Garage's default; -1 when unknown.
    public let tranquility: Int64

    public var id: String { nodeID.isEmpty ? label : nodeID }

    /// What names the node best: Garage forgets a long-gone node's hostname; its tags often name the host.
    public var label: String {
        if !hostname.isEmpty { return hostname }
        if !tags.isEmpty { return tags.joined(separator: ",") }
        return String(nodeID.prefix(16))
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        nodeID = try c.field(.id, "")
        hostname = try c.field(.hostname, "")
        zone = try c.field(.zone, "")
        tags = try c.field(.tags, [])
        kubeNode = try c.field(.kubeNode, "")
        storage = try c.field(.storage, false)
        up = try c.field(.up, false)
        lastSeenSecs = try c.field(.lastSeenSecs, -1)
        draining = try c.field(.draining, false)
        dataAvail = try c.field(.dataAvail, 0)
        dataTotal = try c.field(.dataTotal, 0)
        resyncQueue = try c.field(.resyncQueue, -1)
        resyncErrors = try c.field(.resyncErrors, -1)
        tableSyncQueue = try c.field(.tableSyncQueue, -1)
        statsError = try c.field(.statsError, "")
        tranquility = try c.field(.tranquility, -1)
    }

    private enum CodingKeys: String, CodingKey {
        case id, hostname, zone, tags, kubeNode, storage, up, lastSeenSecs, draining, dataAvail, dataTotal
        case resyncQueue, resyncErrors, tableSyncQueue, statsError, tranquility
    }
}

public enum GarageState: String, Sendable {
    case healthy, degraded, unavailable, unknown

    public var health: ServiceHealth {
        switch self {
        case .healthy: .ok
        case .degraded: .warning
        case .unavailable: .critical
        case .unknown: .unknown
        }
    }
}

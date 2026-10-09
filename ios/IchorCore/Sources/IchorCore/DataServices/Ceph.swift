import Foundation

// Data services: Rook Ceph clusters, pools and OSDs (see DataServices.swift).

public struct CephStatus: Decodable, Equatable, Sendable {
    public let version: String
    public let error: String
    public let clusters: [CephCluster]
    public let pools: [CephPool]
    /// By namespace, then OSD number.
    public let osds: [CephOSD]

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        version = try c.field(.version, "")
        error = try c.field(.error, "")
        clusters = try c.field(.clusters, [])
        pools = try c.field(.pools, [])
        osds = try c.field(.osds, [])
    }

    private enum CodingKeys: String, CodingKey { case version, error, clusters, pools, osds }
}

public struct CephCluster: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    /// Rook's own word: Ready (Connected when external), Progressing, Failure...
    public let phase: String
    public let message: String
    /// HEALTH_OK, HEALTH_WARN or HEALTH_ERR; "" before Ceph reported.
    public let cephHealth: String
    public let health: ServiceHealth
    /// Known reasons only; values from newer cores are dropped.
    public let reasons: [CephReason]
    /// Ceph's health checks, errors first.
    public let checks: [CephCheck]
    public let bytesTotal: Int64
    public let bytesUsed: Int64
    public let osdsUp: Int
    public let osdsTotal: Int
    public let monsReady: Int
    public let monsTotal: Int
    /// Nodes of its OSD and mon pods that are not ready.
    public let notReadyNodes: [String]
    /// Ceph's version, e.g. 19.2.3-0.
    public let version: String
    public let external: Bool

    public var id: String { label }
    public var label: String { "\(namespace)/\(name)" }
    /// Raw capacity used, 0 when Ceph has not reported it.
    public var usedFraction: Double { bytesTotal > 0 ? Double(bytesUsed) / Double(bytesTotal) : 0 }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        phase = try c.field(.phase, "")
        message = try c.field(.message, "")
        cephHealth = try c.field(.cephHealth, "")
        health = try c.wire(.health)
        reasons = try c.wireList(.reasons)
        checks = try c.field(.checks, [])
        bytesTotal = try c.field(.bytesTotal, 0)
        bytesUsed = try c.field(.bytesUsed, 0)
        osdsUp = try c.field(.osdsUp, 0)
        osdsTotal = try c.field(.osdsTotal, 0)
        monsReady = try c.field(.monsReady, 0)
        monsTotal = try c.field(.monsTotal, 0)
        notReadyNodes = try c.field(.notReadyNodes, [])
        version = try c.field(.version, "")
        external = try c.field(.external, false)
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, phase, message, cephHealth, health, reasons, checks, bytesTotal, bytesUsed
        case osdsUp, osdsTotal, monsReady, monsTotal, notReadyNodes, version, external
    }
}

public struct CephCheck: Decodable, Equatable, Identifiable, Sendable {
    /// MON_DOWN, OSD_NEARFULL...
    public let name: String
    /// HEALTH_WARN or HEALTH_ERR.
    public let severity: String
    public let message: String

    public var id: String { name }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.decode(String.self, forKey: .name)
        severity = try c.field(.severity, "")
        message = try c.field(.message, "")
    }

    private enum CodingKeys: String, CodingKey { case name, severity, message }
}

/// A block pool, a filesystem or an object store.
public struct CephPool: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    /// blockPool, filesystem or objectStore.
    public let kind: String
    public let phase: String
    public let health: ServiceHealth

    public var id: String { "\(kind)/\(label)" }
    public var label: String { "\(namespace)/\(name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        kind = try c.field(.kind, "")
        phase = try c.field(.phase, "")
        health = try c.wire(.health)
    }

    private enum CodingKeys: String, CodingKey { case namespace, name, kind, phase, health }
}

public struct CephOSD: Decodable, Equatable, Identifiable, Sendable {
    /// The cluster's.
    public let namespace: String
    /// The ceph-osd-id label ("id" on the wire).
    public let osdID: String
    public let pod: String
    public let node: String
    public let phase: String
    public let ready: Bool

    public var id: String { "\(namespace)/\(pod)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        osdID = try c.field(.osdID, "")
        pod = try c.field(.pod, "")
        node = try c.field(.node, "")
        phase = try c.field(.phase, "")
        ready = try c.field(.ready, false)
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, pod, node, phase, ready
        case osdID = "id"
    }
}

/// Why a Ceph cluster is not ok, as the Go core names it.
public enum CephReason: String, Sendable {
    case healthErr, failure, full, noOSD, noQuorum, healthWarn, nearFull, osds, mons, notReady
}

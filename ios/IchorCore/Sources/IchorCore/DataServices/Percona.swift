import Foundation

// Data services: Percona XtraDB clusters (see DataServices.swift).

public struct PerconaStatus: Decodable, Equatable, Sendable {
    public let version: String
    public let error: String
    public let clusters: [PerconaCluster]

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        version = try c.field(.version, "")
        error = try c.field(.error, "")
        clusters = try c.field(.clusters, [])
    }

    private enum CodingKeys: String, CodingKey { case version, error, clusters }
}

public struct PerconaCluster: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    /// The operator's own word: ready, initializing, paused, stopping, error or unknown.
    public let state: String
    /// The operator's messages, "; "-joined.
    public let message: String
    public let crVersion: String
    public let paused: Bool
    public let health: ServiceHealth
    /// Known reasons only; values from newer cores are dropped.
    public let reasons: [PerconaReason]
    public let pxcSize: Int
    public let pxcReady: Int
    /// haproxy, proxysql or "" for none.
    public let proxy: String
    public let proxySize: Int
    public let proxyReady: Int
    /// The PXC members, by name.
    public let pods: [PerconaPod]
    public let lastBackupAt: Int64
    public let lastBackupFailedAt: Int64
    public let backupSchedules: [PerconaSchedule]

    public var id: String { label }
    public var label: String { "\(namespace)/\(name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        state = try c.field(.state, "")
        message = try c.field(.message, "")
        crVersion = try c.field(.crVersion, "")
        paused = try c.field(.paused, false)
        health = try c.wire(.health)
        reasons = try c.wireList(.reasons)
        pxcSize = try c.field(.pxcSize, 0)
        pxcReady = try c.field(.pxcReady, 0)
        proxy = try c.field(.proxy, "")
        proxySize = try c.field(.proxySize, 0)
        proxyReady = try c.field(.proxyReady, 0)
        pods = try c.field(.pods, [])
        lastBackupAt = try c.field(.lastBackupAt, 0)
        lastBackupFailedAt = try c.field(.lastBackupFailedAt, 0)
        backupSchedules = try c.field(.backupSchedules, [])
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, state, message, crVersion, paused, health, reasons, pxcSize, pxcReady, proxy, proxySize, proxyReady, pods
        case lastBackupAt, lastBackupFailedAt, backupSchedules
    }
}

public typealias PerconaPod = ServicePod

public struct PerconaSchedule: Decodable, Equatable, Sendable {
    public let name: String
    public let schedule: String
    public let keep: Int
    public let storageName: String

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        schedule = try c.field(.schedule, "")
        keep = try c.field(.keep, 0)
        storageName = try c.field(.storageName, "")
    }

    private enum CodingKeys: String, CodingKey { case name, schedule, keep, storageName }
}

/// Why a Percona XtraDB cluster is not ok, as the Go core names it.
public enum PerconaReason: String, Sendable {
    case error, noMember, members, proxy, initializing, backupFailed, backupStale
}

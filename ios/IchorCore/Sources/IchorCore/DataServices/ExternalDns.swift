import Foundation

// Data services: ExternalDNS controllers and the hostnames they publish (see DataServices.swift).

public struct ExternalDnsStatus: Decodable, Equatable, Sendable {
    public let version: String
    public let error: String
    /// One per external-dns Deployment; several are normal (one per provider or zone).
    public let controllers: [ExternalDnsController]
    /// Problems first.
    public let records: [ExternalDnsRecord]
    /// All of them; `records` is capped at 500.
    public let recordCount: Int
    public let truncated: Bool

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        version = try c.field(.version, "")
        error = try c.field(.error, "")
        controllers = try c.field(.controllers, [])
        records = try c.field(.records, [])
        recordCount = try c.field(.recordCount, 0)
        truncated = try c.field(.truncated, false)
    }

    private enum CodingKeys: String, CodingKey { case version, error, controllers, records, recordCount, truncated }
}

public struct ExternalDnsController: Decodable, Equatable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    public let ready: Bool
    public let replicas: Int
    public let readyReplicas: Int
    public let provider: String
    /// sync, upsert-only, create-only; "" is the controller's default.
    public let policy: String
    public let registry: String
    public let ownerId: String
    public let txtPrefix: String
    public let txtSuffix: String
    public let domainFilters: [String]
    public let sources: [String]
    public let dryRun: Bool
    /// --interval in seconds, 60 (its default) when unset.
    public let intervalSeconds: Int
    public let annotationFilter: String
    public let health: ServiceHealth
    /// Known reasons only; values from newer cores are dropped.
    public let reasons: [ExternalDnsReason]

    public var id: String { label }
    public var label: String { "\(namespace)/\(name)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        ready = try c.field(.ready, false)
        replicas = try c.field(.replicas, 0)
        readyReplicas = try c.field(.readyReplicas, 0)
        provider = try c.field(.provider, "")
        policy = try c.field(.policy, "")
        registry = try c.field(.registry, "")
        ownerId = try c.field(.ownerId, "")
        txtPrefix = try c.field(.txtPrefix, "")
        txtSuffix = try c.field(.txtSuffix, "")
        domainFilters = try c.field(.domainFilters, [])
        sources = try c.field(.sources, [])
        dryRun = try c.field(.dryRun, false)
        intervalSeconds = try c.field(.intervalSeconds, 60)
        annotationFilter = try c.field(.annotationFilter, "")
        health = try c.wire(.health)
        reasons = try c.wireList(.reasons)
    }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, ready, replicas, readyReplicas, provider, policy, registry, ownerId
        case txtPrefix, txtSuffix, domainFilters, sources, dryRun, intervalSeconds, annotationFilter, health, reasons
    }
}

/// One hostname a controller is asked to publish, where it comes from and what it should point
/// at. ExternalDNS keeps no status on records: this never says one exists.
public struct ExternalDnsRecord: Decodable, Equatable, Identifiable, Sendable {
    public let dnsName: String
    /// A, AAAA or CNAME.
    public let recordType: String
    public let targets: [String]
    /// Seconds, 0 when unset.
    public let ttl: Int
    /// Ingress, Service, HTTPRoute or DNSEndpoint.
    public let sourceKind: String
    public let sourceNamespace: String
    public let sourceName: String
    /// Open the source object's summary.
    public let sourceGroup: String
    public let sourceVersion: String
    public let sourceResource: String
    /// The covering controller's name, "" when none covers it.
    public let controller: String
    public let health: ServiceHealth
    public let reasons: [ExternalDnsReason]

    public var id: String { "\(dnsName)/\(sourceKind)/\(sourceNamespace)/\(sourceName)" }
    /// "from Ingress demo/app": where the hostname comes from.
    public var sourceLabel: String { "\(sourceKind)/\(sourceNamespace)/\(sourceName)" }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        dnsName = try c.field(.dnsName, "")
        recordType = try c.field(.recordType, "")
        targets = try c.field(.targets, [])
        ttl = try c.field(.ttl, 0)
        sourceKind = try c.field(.sourceKind, "")
        sourceNamespace = try c.field(.sourceNamespace, "")
        sourceName = try c.field(.sourceName, "")
        sourceGroup = try c.field(.sourceGroup, "")
        sourceVersion = try c.field(.sourceVersion, "")
        sourceResource = try c.field(.sourceResource, "")
        controller = try c.field(.controller, "")
        health = try c.wire(.health)
        reasons = try c.wireList(.reasons)
    }

    private enum CodingKeys: String, CodingKey {
        case dnsName, recordType, targets, ttl, sourceKind, sourceNamespace, sourceName
        case sourceGroup, sourceVersion, sourceResource, controller, health, reasons
    }
}

/// Why an ExternalDNS controller or record is not ok, as the Go core names it.
public enum ExternalDnsReason: String, Sendable {
    case notReady, dryRun, stale, noTarget, unmanaged
}

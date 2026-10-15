import Foundation

// Mirrors go/ichorgo/kube_crossplane.go.

/// Crossplane's control plane: packages, the APIs its XRDs define, and every composite, claim
/// and managed resource with its Ready and Synced state, problems first.
public struct Crossplane: Decodable, Equatable, Sendable {
    public let installed: Bool
    public let version: String
    public let error: String
    /// Problems first.
    public let packages: [CrossplanePackage]
    public let definitions: [CrossplaneDefinition]
    public let composites: [CrossplaneObject]
    public let claims: [CrossplaneObject]
    /// Problems first, healthy rows capped (see truncated).
    public let managed: [CrossplaneObject]
    /// Every managed resource, including the omitted healthy ones.
    public let managedTotal: Int
    public let truncated: Bool
    public let counts: CrossplaneCounts

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        installed = try c.field(.installed, false)
        version = try c.field(.version, "")
        error = try c.field(.error, "")
        packages = try c.field(.packages, [])
        definitions = try c.field(.definitions, [])
        composites = try c.field(.composites, [])
        claims = try c.field(.claims, [])
        managed = try c.field(.managed, [])
        managedTotal = try c.field(.managedTotal, 0)
        truncated = try c.field(.truncated, false)
        counts = try c.field(.counts, CrossplaneCounts())
    }

    private enum CodingKeys: String, CodingKey {
        case installed, version, error, packages, definitions, composites, claims, managed, managedTotal, truncated, counts
    }
}

public struct CrossplaneCounts: Decodable, Equatable, Sendable {
    public let ok: Int
    public let warning: Int
    public let critical: Int

    public init(ok: Int = 0, warning: Int = 0, critical: Int = 0) {
        self.ok = ok
        self.warning = warning
        self.critical = critical
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        ok = try c.field(.ok, 0)
        warning = try c.field(.warning, 0)
        critical = try c.field(.critical, 0)
    }

    private enum CodingKeys: String, CodingKey { case ok, warning, critical }
}

/// Where an object lives: enough for the object summary (group, version, resource, kind).
public struct CrossplaneRef: Decodable, Equatable, Hashable, Sendable {
    public let group: String
    public let version: String
    public let resource: String
    public let kind: String
    public let namespace: String
    public let name: String

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        group = try c.field(.group, "")
        version = try c.field(.version, "")
        resource = try c.field(.resource, "")
        kind = try c.field(.kind, "")
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
    }

    private enum CodingKeys: String, CodingKey { case group, version, resource, kind, namespace, name }
}

public struct CrossplaneCondition: Decodable, Equatable, Sendable {
    public let status: String
    public let reason: String
    public let message: String
    /// Unix ms, 0 when unknown.
    public let lastTransition: Int64

    public init(status: String = "", reason: String = "", message: String = "", lastTransition: Int64 = 0) {
        self.status = status
        self.reason = reason
        self.message = message
        self.lastTransition = lastTransition
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        status = try c.field(.status, "")
        reason = try c.field(.reason, "")
        message = try c.field(.message, "")
        lastTransition = try c.field(.lastTransition, 0)
    }

    private enum CodingKeys: String, CodingKey { case status, reason, message, lastTransition }
}

public struct CrossplanePackage: Decodable, Equatable, Identifiable, Sendable {
    public let ref: CrossplaneRef
    /// The package image, as written.
    public let package: String
    public let currentRevision: String
    public let currentIdentifier: String
    public let installed: CrossplaneCondition
    public let healthy: CrossplaneCondition
    public let health: ServiceHealth
    /// Known reasons only; values from newer cores are dropped.
    public let reasons: [CrossplaneReason]

    public var id: String { label }
    public var label: String { ref.namespace.isEmpty ? ref.name : "\(ref.namespace)/\(ref.name)" }

    public init(from decoder: Decoder) throws {
        ref = try CrossplaneRef(from: decoder)
        let c = try decoder.container(keyedBy: CodingKeys.self)
        package = try c.field(.package, "")
        currentRevision = try c.field(.currentRevision, "")
        currentIdentifier = try c.field(.currentIdentifier, "")
        installed = try c.field(.installed, CrossplaneCondition(status: "", reason: "", message: "", lastTransition: 0))
        healthy = try c.field(.healthy, CrossplaneCondition(status: "", reason: "", message: "", lastTransition: 0))
        health = try c.wire(.health)
        reasons = try c.wireList(.reasons)
    }

    private enum CodingKeys: String, CodingKey {
        case package, currentRevision, currentIdentifier, installed, healthy, health, reasons
    }
}

public struct CrossplaneDefinitionVersion: Decodable, Equatable, Sendable {
    public let name: String
    public let served: Bool
    public let referenceable: Bool
}

public struct CrossplaneDefinition: Decodable, Equatable, Identifiable, Sendable {
    public let ref: CrossplaneRef
    /// The API the XRD defines.
    public let compositeGroup: String
    public let compositeKind: String
    public let plural: String
    /// LegacyCluster, Namespaced or Cluster.
    public let scope: String
    public let claimKind: String
    public let claimPlural: String
    public let versions: [CrossplaneDefinitionVersion]
    public let established: CrossplaneCondition
    public let offered: CrossplaneCondition
    public let compositions: Int
    public let compositionNames: [String]
    public let composites: Int
    public let health: ServiceHealth
    public let reasons: [CrossplaneReason]

    public var id: String { label }
    public var label: String { ref.namespace.isEmpty ? ref.name : "\(ref.namespace)/\(ref.name)" }

    public init(from decoder: Decoder) throws {
        ref = try CrossplaneRef(from: decoder)
        let c = try decoder.container(keyedBy: CodingKeys.self)
        compositeGroup = try c.field(.compositeGroup, "")
        compositeKind = try c.field(.compositeKind, "")
        plural = try c.field(.plural, "")
        scope = try c.field(.scope, "")
        claimKind = try c.field(.claimKind, "")
        claimPlural = try c.field(.claimPlural, "")
        versions = try c.field(.versions, [])
        established = try c.field(.established, CrossplaneCondition(status: "", reason: "", message: "", lastTransition: 0))
        offered = try c.field(.offered, CrossplaneCondition(status: "", reason: "", message: "", lastTransition: 0))
        compositions = try c.field(.compositions, 0)
        compositionNames = try c.field(.compositionNames, [])
        composites = try c.field(.composites, 0)
        health = try c.wire(.health)
        reasons = try c.wireList(.reasons)
    }

    private enum CodingKeys: String, CodingKey {
        case compositeGroup, compositeKind, plural, scope, claimKind, claimPlural, versions, established, offered
        case compositions, compositionNames, composites, health, reasons
    }
}

/// A composite resource, a claim or a managed resource: the fields they share.
public struct CrossplaneObject: Decodable, Equatable, Identifiable, Sendable {
    public let ref: CrossplaneRef
    /// Composites and claims: the composition rendering them, and the composite a claim binds to.
    public let composition: String
    public let resources: Int
    public let claim: String
    public let composite: String
    /// Managed resources.
    public let providerConfig: String
    public let externalName: String
    public let paused: Bool
    public let ready: CrossplaneCondition
    public let synced: CrossplaneCondition
    /// Unix ms, 0 when unknown.
    public let lastTransition: Int64
    public let health: ServiceHealth
    public let reasons: [CrossplaneReason]

    public var id: String { label }
    public var label: String { ref.namespace.isEmpty ? ref.name : "\(ref.namespace)/\(ref.name)" }

    public init(from decoder: Decoder) throws {
        ref = try CrossplaneRef(from: decoder)
        let c = try decoder.container(keyedBy: CodingKeys.self)
        composition = try c.field(.composition, "")
        resources = try c.field(.resources, 0)
        claim = try c.field(.claim, "")
        composite = try c.field(.composite, "")
        providerConfig = try c.field(.providerConfig, "")
        externalName = try c.field(.externalName, "")
        paused = try c.field(.paused, false)
        ready = try c.field(.ready, CrossplaneCondition(status: "", reason: "", message: "", lastTransition: 0))
        synced = try c.field(.synced, CrossplaneCondition(status: "", reason: "", message: "", lastTransition: 0))
        lastTransition = try c.field(.lastTransition, 0)
        health = try c.wire(.health)
        reasons = try c.wireList(.reasons)
    }

    private enum CodingKeys: String, CodingKey {
        case composition, resources, claim, composite, providerConfig, externalName, paused, ready, synced, lastTransition, health, reasons
    }
}

/// Why a Crossplane row is not ok, as the Go core names it.
public enum CrossplaneReason: String, Sendable {
    case syncFailed, providerUnhealthy, notReady, deleting, paused, notEstablished
}

public extension Crossplane {
    /// Every composite, claim and managed row, problems first as the Go core sorted them.
    var rows: [CrossplaneObject] { composites + claims + managed }

    /// The rows whose health is not ok, across kinds.
    var problems: [CrossplaneObject] { rows.filter { $0.health.needsAttention } }
}

/// The screen's filter row.
public enum CrossplaneFilter: String, CaseIterable, Sendable {
    case all, critical, warning
}

public extension Array where Element == CrossplaneObject {
    /// The rows matching the filter and the query (kind, name, external name).
    func filtered(_ filter: CrossplaneFilter, _ query: String) -> [CrossplaneObject] {
        let q = query.trimmingCharacters(in: .whitespaces)
        return self.filter { o in
            let match: Bool
            switch filter {
            case .all: match = true
            case .critical: match = o.health == .critical
            case .warning: match = o.health == .warning
            }
            return match && (q.isEmpty
                || o.ref.kind.localizedCaseInsensitiveContains(q)
                || o.ref.name.localizedCaseInsensitiveContains(q)
                || o.externalName.localizedCaseInsensitiveContains(q))
        }
    }
}

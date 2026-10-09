import Foundation

/// The Storage screen (Go `KubeStorage`): each PersistentVolumeClaim with its volume, class,
/// pods and fill, problems first. `partialAccess`: volumes, classes or nodes could not be read.
public struct KubeStorage: Decodable, Sendable {
    public let claims: [StorageClaim]
    public let partialAccess: Bool

    private enum CodingKeys: String, CodingKey { case claims, partialAccess }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        claims = try c.field(.claims, [])
        partialAccess = try c.field(.partialAccess, false)
    }
}

/// How a claim is doing: ok, warning (pending, terminating, filling), critical (lost, nearly full).
public enum StorageLevel: String, WireEnum, Sendable {
    case ok, warning, critical
    public static let wireFallback = StorageLevel.ok
}

/// A claim. Capacity and used in bytes; `measured`: a kubelet reported the fill.
public struct StorageClaim: Decodable, Hashable, Identifiable, Sendable {
    public let namespace: String
    public let name: String
    public let phase: String
    public let storageClass: String
    public let provisioner: String
    public let volume: String
    public let reclaimPolicy: String
    public let accessModes: [String]
    public let capacity: Double
    public let used: Double
    public let usedPercent: Double
    public let measured: Bool
    public let pods: [String]
    public let terminating: Bool
    /// The data service the volume belongs to, by its catalog id; "" for none.
    public let managedBy: String
    public let level: StorageLevel

    public var id: String { "\(namespace)/\(name)" }
    public var usedFraction: Double { min(max(usedPercent / 100, 0), 1) }
    /// The data service screen that manages this volume; nil for none this version knows.
    public var managedKind: DataServiceKind? { DataServiceKind(rawValue: managedBy) }

    private enum CodingKeys: String, CodingKey {
        case namespace, name, phase, storageClass, provisioner, volume, reclaimPolicy, accessModes
        case capacity, used, usedPercent, measured, pods, terminating, managedBy, level
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        namespace = try c.field(.namespace, "")
        name = try c.field(.name, "")
        phase = try c.field(.phase, "")
        storageClass = try c.field(.storageClass, "")
        provisioner = try c.field(.provisioner, "")
        volume = try c.field(.volume, "")
        reclaimPolicy = try c.field(.reclaimPolicy, "")
        accessModes = try c.field(.accessModes, [])
        capacity = try c.field(.capacity, 0)
        used = try c.field(.used, 0)
        usedPercent = try c.field(.usedPercent, 0)
        measured = try c.field(.measured, false)
        pods = try c.field(.pods, [])
        terminating = try c.field(.terminating, false)
        managedBy = try c.field(.managedBy, "")
        level = try c.wire(.level)
    }
}

/// The claims whose namespace/name, class, volume, provisioner or a pod contains `query`.
public func filterStorageClaims(_ claims: [StorageClaim], query: String) -> [StorageClaim] {
    let q = query.trimmingCharacters(in: .whitespaces)
    guard !q.isEmpty else { return claims }
    return claims.filter { c in
        [c.id, c.storageClass, c.volume, c.provisioner].contains { $0.localizedCaseInsensitiveContains(q) }
            || c.pods.contains { $0.localizedCaseInsensitiveContains(q) }
    }
}

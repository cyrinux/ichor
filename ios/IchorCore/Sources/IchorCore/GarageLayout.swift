import Foundation

// Mirrors go/ichorgo/kube_garage_layout.go: a Garage cluster's layout (who holds which role),
// what is staged for its next version and what applying it would do, and its version history.

/// Wire value of a gateway role's capacity: a gateway stores nothing.
public let garageGatewayCapacity: Int64 = -1

/// The least capacity Garage accepts for a storage role, in bytes.
public let garageMinCapacity: Int64 = 1024

public struct GarageLayout: Decodable, Equatable, Sendable {
    public let version: Int64
    /// version + 1 while something is staged, else 0.
    public let nextVersion: Int64
    public let partitionSize: Int64
    /// "maximum" or a number of zones.
    public let zoneRedundancy: String
    /// "" when not staged.
    public let stagedZoneRedundancy: String
    /// Only while something is staged.
    public let preview: GarageLayoutPreview?
    public let nodes: [GarageLayoutNode]
    /// Newest first.
    public let versions: [GarageLayoutVersion]
    public let minAck: Int64
    /// Only while several versions are live.
    public let trackers: [GarageLayoutTracker]
    /// A draining version waits on a node that is down.
    public let stuck: Bool
    /// Garage's own lines after an apply.
    public let messages: [String]

    public var hasStaged: Bool { nextVersion > 0 }
    /// The nodes with a role in the current layout.
    public var members: [GarageLayoutNode] { nodes.filter { $0.role != nil } }
    /// The nodes whose role changes in the next version.
    public var staged: [GarageLayoutNode] { nodes.filter { $0.staged != .unchanged } }
    /// Known nodes without a role and nothing staged: they can join.
    public var candidates: [GarageLayoutNode] { nodes.filter { $0.role == nil && $0.staged == .unchanged && $0.known } }
    public var storageNodes: Int { members.filter { !($0.role?.gateway ?? true) }.count }
    public var gatewayNodes: Int { members.filter { $0.role?.gateway ?? false }.count }
    /// Every zone a role names, current or staged, for the editor's suggestions.
    public var zones: [String] {
        Array(Set(nodes.compactMap { $0.effectiveRole?.zone }.filter { !$0.isEmpty })).sorted()
    }

    public func node(_ id: String) -> GarageLayoutNode? { nodes.first { $0.id == id } }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        version = try c.field(.version, 0)
        nextVersion = try c.field(.nextVersion, 0)
        partitionSize = try c.field(.partitionSize, 0)
        zoneRedundancy = try c.field(.zoneRedundancy, "")
        stagedZoneRedundancy = try c.field(.stagedZoneRedundancy, "")
        preview = try c.field(.preview, nil)
        nodes = try c.field(.nodes, [])
        versions = try c.field(.versions, [])
        minAck = try c.field(.minAck, 0)
        trackers = try c.field(.trackers, [])
        stuck = try c.field(.stuck, false)
        messages = try c.field(.messages, [])
    }

    private enum CodingKeys: String, CodingKey {
        case version, nextVersion, partitionSize, zoneRedundancy, stagedZoneRedundancy, preview, nodes, versions,
            minAck, trackers, stuck, messages
    }
}

public struct GarageLayoutPreview: Decodable, Equatable, Sendable {
    public let ok: Bool
    public let error: String
    public let messages: [String]
    /// -1 when Garage did not say.
    public let movedPartitions: Int64
    public let usableCapacity: Int64
    public let effectiveCapacity: Int64
    public let totalCapacity: Int64
    public let lowUsableCapacity: Bool
    public let lowPartitionSize: Bool

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        ok = try c.field(.ok, false)
        error = try c.field(.error, "")
        messages = try c.field(.messages, [])
        movedPartitions = try c.field(.movedPartitions, -1)
        usableCapacity = try c.field(.usableCapacity, 0)
        effectiveCapacity = try c.field(.effectiveCapacity, 0)
        totalCapacity = try c.field(.totalCapacity, 0)
        lowUsableCapacity = try c.field(.lowUsableCapacity, false)
        lowPartitionSize = try c.field(.lowPartitionSize, false)
    }

    private enum CodingKeys: String, CodingKey {
        case ok, error, messages, movedPartitions, usableCapacity, effectiveCapacity, totalCapacity, lowUsableCapacity,
            lowPartitionSize
    }
}

public struct GarageLayoutRole: Decodable, Equatable, Sendable {
    public let zone: String
    /// Bytes, `garageGatewayCapacity` for a gateway.
    public let capacity: Int64
    public let tags: [String]
    public let storedPartitions: Int64
    public let usableCapacity: Int64

    public var gateway: Bool { capacity < 0 }
    /// How much of the declared capacity the layout uses, 0 when unknown or a gateway.
    public var usableFraction: Double {
        guard capacity > 0, usableCapacity > 0 else { return 0 }
        return min(1, Double(usableCapacity) / Double(capacity))
    }

    public init(zone: String, capacity: Int64, tags: [String], storedPartitions: Int64 = 0, usableCapacity: Int64 = 0) {
        self.zone = zone
        self.capacity = capacity
        self.tags = tags
        self.storedPartitions = storedPartitions
        self.usableCapacity = usableCapacity
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        zone = try c.field(.zone, "")
        capacity = try c.field(.capacity, garageGatewayCapacity)
        tags = try c.field(.tags, [])
        storedPartitions = try c.field(.storedPartitions, 0)
        usableCapacity = try c.field(.usableCapacity, 0)
    }

    private enum CodingKeys: String, CodingKey { case zone, capacity, tags, storedPartitions, usableCapacity }
}

/// What is staged for a node in the next layout version.
public enum GarageStagedChange: String, WireEnum, Sendable {
    case unchanged = "none"
    case add, change, remove
    public static let wireFallback = GarageStagedChange.unchanged
}

public struct GarageLayoutNode: Decodable, Equatable, Identifiable, Sendable {
    public let id: String
    public let hostname: String
    public let kubeNode: String
    public let up: Bool
    /// The cluster still knows the node (it is in GetClusterStatus).
    public let known: Bool
    public let draining: Bool
    public let dataTotal: Int64
    public let role: GarageLayoutRole?
    public let staged: GarageStagedChange
    public let stagedRole: GarageLayoutRole?

    public var label: String { hostname.isEmpty ? String(id.prefix(16)) : hostname }

    /// The role after the staged changes: none when the node leaves.
    public var effectiveRole: GarageLayoutRole? {
        switch staged {
        case .remove: nil
        case .unchanged: role
        case .add, .change: stagedRole ?? role
        }
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        hostname = try c.field(.hostname, "")
        kubeNode = try c.field(.kubeNode, "")
        up = try c.field(.up, false)
        known = try c.field(.known, false)
        draining = try c.field(.draining, false)
        dataTotal = try c.field(.dataTotal, 0)
        role = try c.field(.role, nil)
        staged = try c.wire(.staged)
        stagedRole = try c.field(.stagedRole, nil)
    }

    private enum CodingKeys: String, CodingKey {
        case id, hostname, kubeNode, up, known, draining, dataTotal, role, staged, stagedRole
    }
}

public enum GarageLayoutStatus: String, WireEnum, Sendable {
    case current, draining, historical, unknown
    public static let wireFallback = GarageLayoutStatus.unknown
}

public struct GarageLayoutVersion: Decodable, Equatable, Identifiable, Sendable {
    public let version: Int64
    public let status: GarageLayoutStatus
    public let storageNodes: Int64
    public let gatewayNodes: Int64

    public var id: Int64 { version }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        version = try c.field(.version, 0)
        status = try c.wire(.status)
        storageNodes = try c.field(.storageNodes, 0)
        gatewayNodes = try c.field(.gatewayNodes, 0)
    }

    private enum CodingKeys: String, CodingKey { case version, status, storageNodes, gatewayNodes }
}

public struct GarageLayoutTracker: Decodable, Equatable, Identifiable, Sendable {
    public let id: String
    public let hostname: String
    public let ack: Int64
    public let sync: Int64
    public let syncAck: Int64

    public var label: String { hostname.isEmpty ? String(id.prefix(16)) : hostname }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        hostname = try c.field(.hostname, "")
        ack = try c.field(.ack, 0)
        sync = try c.field(.sync, 0)
        syncAck = try c.field(.syncAck, 0)
    }

    private enum CodingKeys: String, CodingKey { case id, hostname, ack, sync, syncAck }
}

/// The units the role editor offers for a capacity; Garage shows IEC units too.
public enum GarageCapacityUnit: String, CaseIterable, Sendable {
    case gib = "GiB"
    case tib = "TiB"

    public var bytes: Int64 {
        switch self {
        case .gib: 1 << 30
        case .tib: 1 << 40
        }
    }

    /// The unit that shows `bytes` with the fewest digits: TiB from one tebibyte up.
    public static func forBytes(_ bytes: Int64) -> GarageCapacityUnit {
        bytes >= GarageCapacityUnit.tib.bytes ? .tib : .gib
    }
}

/// "1.5" in `unit` as bytes, nil when `text` is not a positive number.
public func garageCapacityBytes(_ text: String, unit: GarageCapacityUnit) -> Int64? {
    let cleaned = text.trimmingCharacters(in: .whitespaces).replacingOccurrences(of: ",", with: ".")
    guard let value = Double(cleaned), value > 0, value.isFinite else { return nil }
    let bytes = value * Double(unit.bytes)
    guard bytes < Double(Int64.max / 2) else { return nil }
    return Int64(bytes)
}

/// `bytes` in `unit`, with up to two decimals and no trailing zeros: 1.5, 450, 0.25.
public func garageCapacityText(_ bytes: Int64, unit: GarageCapacityUnit) -> String {
    let value = Double(bytes) / Double(unit.bytes)
    let rounded = (value * 100).rounded() / 100
    if rounded == rounded.rounded(.down) { return String(Int64(rounded)) }
    return String(rounded)
}

/// The capacity to propose for a node joining the layout: its data disk rounded down to a
/// whole unit, below the disk so Garage keeps room for metadata; 0 when the disk is unknown.
public func garageSuggestedCapacity(dataTotal: Int64) -> Int64 {
    let gib = GarageCapacityUnit.gib.bytes
    guard dataTotal >= gib else { return 0 }
    let unit = GarageCapacityUnit.forBytes(dataTotal)
    let whole = dataTotal / unit.bytes
    // A disk of exactly N units proposes N-1: the disk is never fully given to Garage.
    let units = dataTotal % unit.bytes == 0 ? whole - 1 : whole
    if units <= 0 { return max(1, dataTotal / gib - 1) * gib }
    return units * unit.bytes
}

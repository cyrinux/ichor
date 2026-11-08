import Foundation

// Mirrors go/ichorgo diskLayoutList (NodeDisks; Talos v1.8+). Same model as Android Disks.kt.

/// Each physical disk of a node with its partitions, as the Storage screen's "Disks" section draws them.
public struct NodeDisks: Decodable, Equatable, Sendable {
    /// False on a Talos without block resources.
    public let supported: Bool
    public let reason: String
    public let disks: [DiskLayout]

    public init(supported: Bool, reason: String = "", disks: [DiskLayout] = []) {
        self.supported = supported
        self.reason = reason
        self.disks = disks
    }

    private enum CodingKeys: String, CodingKey { case supported, reason, disks }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        supported = try c.field(.supported, true)
        reason = try c.field(.reason, "")
        disks = try c.field(.disks, [])
    }
}

public struct DiskLayout: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    public let devPath: String
    /// Bytes.
    public let size: UInt64
    /// ssd | hdd | nvme | sd | unknown
    public let type: String
    /// nvme, sata, virtio, usb... "" when unknown.
    public let transport: String
    public let model: String
    public let serial: String
    public let systemDisk: Bool
    public let readonly: Bool
    /// Bytes not in any partition.
    public let unallocated: UInt64
    /// By partition index.
    public let partitions: [DiskPartition]
    /// The bar, by offset: what the core cut, drawn as given.
    public let segments: [DiskSegment]

    public var id: String { name }

    public init(
        name: String, devPath: String = "", size: UInt64 = 0, type: String = "", transport: String = "",
        model: String = "", serial: String = "", systemDisk: Bool = false, readonly: Bool = false,
        unallocated: UInt64 = 0, partitions: [DiskPartition] = [], segments: [DiskSegment] = []
    ) {
        self.name = name
        self.devPath = devPath
        self.size = size
        self.type = type
        self.transport = transport
        self.model = model
        self.serial = serial
        self.systemDisk = systemDisk
        self.readonly = readonly
        self.unallocated = unallocated
        self.partitions = partitions
        self.segments = segments
    }

    private enum CodingKeys: String, CodingKey {
        case name, devPath, size, type, transport, model, serial, systemDisk, readonly, unallocated, partitions,
            segments
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        devPath = try c.field(.devPath, "")
        size = try c.field(.size, 0)
        type = try c.field(.type, "")
        transport = try c.field(.transport, "")
        model = try c.field(.model, "")
        serial = try c.field(.serial, "")
        systemDisk = try c.field(.systemDisk, false)
        readonly = try c.field(.readonly, false)
        unallocated = try c.field(.unallocated, 0)
        partitions = try c.field(.partitions, [])
        segments = try c.field(.segments, [])
    }
}

public struct DiskPartition: Decodable, Equatable, Identifiable, Sendable {
    public let index: Int
    public let devPath: String
    public let label: String
    /// The Talos volume on it (STATE, EPHEMERAL...), "" when none.
    public let volumeId: String
    public let offset: UInt64
    public let size: UInt64
    public let filesystem: String
    /// enabled | disabled | unknown
    public let encryption: String
    public let encryptionProvider: String
    public let uuid: String
    public let partitionUuid: String
    public let partitionType: String
    public let mountedOn: String
    public let phase: String
    public let error: String

    public var id: Int { index }

    public init(
        index: Int, devPath: String = "", label: String = "", volumeId: String = "", offset: UInt64 = 0,
        size: UInt64 = 0, filesystem: String = "", encryption: String = "", encryptionProvider: String = "",
        uuid: String = "", partitionUuid: String = "", partitionType: String = "", mountedOn: String = "",
        phase: String = "", error: String = ""
    ) {
        self.index = index
        self.devPath = devPath
        self.label = label
        self.volumeId = volumeId
        self.offset = offset
        self.size = size
        self.filesystem = filesystem
        self.encryption = encryption
        self.encryptionProvider = encryptionProvider
        self.uuid = uuid
        self.partitionUuid = partitionUuid
        self.partitionType = partitionType
        self.mountedOn = mountedOn
        self.phase = phase
        self.error = error
    }

    private enum CodingKeys: String, CodingKey {
        case index, devPath, label, volumeId, offset, size, filesystem, encryption, encryptionProvider
        case uuid, partitionUuid, partitionType, mountedOn, phase, error
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        index = try c.field(.index, 0)
        devPath = try c.field(.devPath, "")
        label = try c.field(.label, "")
        volumeId = try c.field(.volumeId, "")
        offset = try c.field(.offset, 0)
        size = try c.field(.size, 0)
        filesystem = try c.field(.filesystem, "")
        encryption = try c.field(.encryption, "")
        encryptionProvider = try c.field(.encryptionProvider, "")
        uuid = try c.field(.uuid, "")
        partitionUuid = try c.field(.partitionUuid, "")
        partitionType = try c.field(.partitionType, "")
        mountedOn = try c.field(.mountedOn, "")
        phase = try c.field(.phase, "")
        error = try c.field(.error, "")
    }

    /// The partition's label, else its Talos volume, else nil (the view says "Partition N").
    public var knownLabel: String? {
        if !label.trimmingCharacters(in: .whitespaces).isEmpty { return label }
        if !volumeId.trimmingCharacters(in: .whitespaces).isEmpty { return volumeId }
        return nil
    }

    public var encryptionState: EncryptionState { EncryptionState(wire: encryption) }

    /// "ready" is the only phase of a usable volume; a partition without one has nothing to say.
    public var phaseNeedsMention: Bool { !phase.isEmpty && phase.lowercased() != "ready" }
}

public enum EncryptionState: String, WireEnum, Sendable {
    case enabled, disabled, unknown
    public static let wireFallback = EncryptionState.unknown
}

public struct DiskSegment: Decodable, Equatable, Sendable {
    /// partition | free
    public let kind: String
    public let index: Int
    public let label: String
    public let offset: UInt64
    public let size: UInt64

    public init(kind: String, index: Int = 0, label: String = "", offset: UInt64 = 0, size: UInt64 = 0) {
        self.kind = kind
        self.index = index
        self.label = label
        self.offset = offset
        self.size = size
    }

    private enum CodingKeys: String, CodingKey { case kind, index, label, offset, size }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.field(.kind, "")
        index = try c.field(.index, 0)
        label = try c.field(.label, "")
        offset = try c.field(.offset, 0)
        size = try c.field(.size, 0)
    }

    public var isFree: Bool { kind == "free" }
}

/// One slice of a disk's bar: `fraction` of the bar's width, all slices summing to 1.
public struct BarSlice: Equatable, Sendable {
    public let segment: DiskSegment
    public let fraction: Double
}

/// The disk's segments as bar slices sized by their share of the segments' total (the core
/// starts the bar at the first partition, so the total is a bit under the disk size). Slices
/// under `minFraction` are widened to it, the others shrunk to keep the sum at 1, so a 1 MiB
/// META partition stays visible next to a 100 GiB EPHEMERAL. Empty when nothing to draw.
/// Same rule as Android `barFractions`.
public func barSlices(_ disk: DiskLayout, minFraction: Double = 0.015) -> [BarSlice] {
    let drawn = disk.segments.filter { $0.size > 0 }
    let total = drawn.reduce(0.0) { $0 + Double($1.size) }
    guard total > 0 else { return [] }
    let raw = drawn.map { Double($0.size) / total }
    let floor = min(minFraction, 1.0 / Double(drawn.count))
    let smallCount = raw.filter { $0 < floor }.count
    let bigTotal = raw.filter { $0 >= floor }.reduce(0, +)
    let room = 1.0 - floor * Double(smallCount)
    return zip(drawn, raw).map { segment, share in
        BarSlice(segment: segment, fraction: share < floor ? floor : share / bigTotal * room)
    }
}

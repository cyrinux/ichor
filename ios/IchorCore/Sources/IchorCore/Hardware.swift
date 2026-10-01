import Foundation

/// SMBIOS system/CPU/memory, disks, extensions and security state of a node (NodeHardware);
/// each section is best-effort and a failed one is named in `errors`
/// (system, processors, memory, disks, extensions, security).
public struct NodeHardware: Decodable, Equatable, Sendable {
    public let system: SystemInfo?
    public let processors: [ProcessorInfo]
    public let memory: [MemoryModule]
    public let disks: [DiskInfo]
    public let extensions: [ExtensionInfo]
    public let security: SecurityInfo?
    public let errors: [String: String]

    public init(system: SystemInfo? = nil, processors: [ProcessorInfo] = [], memory: [MemoryModule] = [], disks: [DiskInfo] = [],
                extensions: [ExtensionInfo] = [], security: SecurityInfo? = nil, errors: [String: String] = [:]) {
        self.system = system
        self.processors = processors
        self.memory = memory
        self.disks = disks
        self.extensions = extensions
        self.security = security
        self.errors = errors
    }

    private enum CodingKeys: String, CodingKey { case system, processors, memory, disks, extensions, security, errors }

    // Go encodes empty (nil) slices and maps as null.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        system = try c.decodeIfPresent(SystemInfo.self, forKey: .system)
        processors = try c.decodeIfPresent([ProcessorInfo].self, forKey: .processors) ?? []
        memory = try c.decodeIfPresent([MemoryModule].self, forKey: .memory) ?? []
        disks = try c.decodeIfPresent([DiskInfo].self, forKey: .disks) ?? []
        extensions = try c.decodeIfPresent([ExtensionInfo].self, forKey: .extensions) ?? []
        security = try c.decodeIfPresent(SecurityInfo.self, forKey: .security)
        errors = try c.decodeIfPresent([String: String].self, forKey: .errors) ?? [:]
    }

    /// Installed memory in bytes (sum of the populated modules).
    public var totalMemoryBytes: UInt64 { memory.reduce(UInt64(0)) { $0 &+ UInt64($1.sizeMib) * 1_048_576 } }

    public var totalCores: UInt32 { processors.reduce(0) { $0 &+ $1.cores } }
    public var totalThreads: UInt32 { processors.reduce(0) { $0 &+ $1.threads } }
}

public struct SystemInfo: Decodable, Equatable, Sendable {
    public let manufacturer: String
    public let product: String
    public let version: String
    public let serial: String
    public let uuid: String
    public let sku: String
    public let biosVersion: String

    public init(manufacturer: String = "", product: String = "", version: String = "", serial: String = "",
                uuid: String = "", sku: String = "", biosVersion: String = "") {
        self.manufacturer = manufacturer
        self.product = product
        self.version = version
        self.serial = serial
        self.uuid = uuid
        self.sku = sku
        self.biosVersion = biosVersion
    }
}

public struct ProcessorInfo: Decodable, Equatable, Identifiable, Sendable {
    public let socket: String
    public let manufacturer: String
    public let model: String
    public let cores: UInt32
    public let threads: UInt32
    public let maxSpeedMhz: UInt32
    public let bootSpeedMhz: UInt32

    public var id: String { "\(socket)/\(model)" }

    public init(socket: String = "CPU 0", manufacturer: String = "", model: String = "", cores: UInt32 = 0,
                threads: UInt32 = 0, maxSpeedMhz: UInt32 = 0, bootSpeedMhz: UInt32 = 0) {
        self.socket = socket
        self.manufacturer = manufacturer
        self.model = model
        self.cores = cores
        self.threads = threads
        self.maxSpeedMhz = maxSpeedMhz
        self.bootSpeedMhz = bootSpeedMhz
    }
}

public struct MemoryModule: Decodable, Equatable, Identifiable, Sendable {
    /// Device locator, e.g. DIMM 0.
    public let slot: String
    public let bank: String
    public let sizeMib: UInt32
    public let type: String
    /// MT/s.
    public let speed: UInt32
    public let manufacturer: String
    public let serial: String

    public var id: String { "\(slot)/\(bank)/\(serial)" }

    public init(slot: String, bank: String = "", sizeMib: UInt32, type: String = "", speed: UInt32 = 0,
                manufacturer: String = "", serial: String = "") {
        self.slot = slot
        self.bank = bank
        self.sizeMib = sizeMib
        self.type = type
        self.speed = speed
        self.manufacturer = manufacturer
        self.serial = serial
    }
}

public struct DiskInfo: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    public let devPath: String
    public let model: String
    public let serial: String
    /// Bytes.
    public let size: UInt64
    /// ssd | hdd | nvme | sd | unknown
    public let type: String
    public let wwid: String
    public let busPath: String
    public let systemDisk: Bool
    public let readonly: Bool

    public var id: String { name }

    public init(name: String, devPath: String = "", model: String = "", serial: String = "", size: UInt64 = 0, type: String = "ssd",
                wwid: String = "", busPath: String = "", systemDisk: Bool = false, readonly: Bool = false) {
        self.name = name
        self.devPath = devPath
        self.model = model
        self.serial = serial
        self.size = size
        self.type = type
        self.wwid = wwid
        self.busPath = busPath
        self.systemDisk = systemDisk
        self.readonly = readonly
    }
}

public struct ExtensionInfo: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    public let version: String
    public let author: String
    public let description: String

    public var id: String { name }

    public init(name: String, version: String = "", author: String = "", description: String = "") {
        self.name = name
        self.version = version
        self.author = author
        self.description = description
    }
}

public struct SecurityInfo: Decodable, Equatable, Sendable {
    public let secureBoot: Bool
    public let bootedWithUki: Bool
    public let ukiSigningKeyFingerprint: String
    public let pcrSigningKeyFingerprint: String
    public let selinuxState: String
    public let fipsState: String
    public let moduleSignatureEnforced: Bool

    public init(secureBoot: Bool = false, bootedWithUki: Bool = false, ukiSigningKeyFingerprint: String = "",
                pcrSigningKeyFingerprint: String = "", selinuxState: String = "", fipsState: String = "",
                moduleSignatureEnforced: Bool = false) {
        self.secureBoot = secureBoot
        self.bootedWithUki = bootedWithUki
        self.ukiSigningKeyFingerprint = ukiSigningKeyFingerprint
        self.pcrSigningKeyFingerprint = pcrSigningKeyFingerprint
        self.selinuxState = selinuxState
        self.fipsState = fipsState
        self.moduleSignatureEnforced = moduleSignatureEnforced
    }
}

/// Disks with the system (Talos install) disk first, then by name.
public func sortedDisks(_ disks: [DiskInfo]) -> [DiskInfo] {
    disks.sorted { a, b in
        if a.systemDisk != b.systemDisk { return a.systemDisk }
        return a.name < b.name
    }
}

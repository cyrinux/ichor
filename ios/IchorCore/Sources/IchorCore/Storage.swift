import Foundation

/// A mounted filesystem of a node (NodeMounts).
public struct MountInfo: Decodable, Equatable, Identifiable, Sendable {
    public let filesystem: String
    public let mountedOn: String
    /// Bytes.
    public let size: UInt64
    public let available: UInt64
    public let used: UInt64
    /// 0–100.
    public let usedPercent: Double

    public var id: String { "\(mountedOn)\n\(filesystem)" }

    public init(filesystem: String, mountedOn: String, size: UInt64 = 0, available: UInt64 = 0, used: UInt64 = 0, usedPercent: Double = 0) {
        self.filesystem = filesystem
        self.mountedOn = mountedOn
        self.size = size
        self.available = available
        self.used = used
        self.usedPercent = usedPercent
    }

    private enum CodingKeys: String, CodingKey { case filesystem, mountedOn, size, available, used, usedPercent }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        filesystem = try c.field(.filesystem, "")
        mountedOn = try c.field(.mountedOn, "")
        size = try c.field(.size, 0)
        available = try c.field(.available, 0)
        used = try c.field(.used, (size > available ? size - available : 0))
        usedPercent = try c.field(.usedPercent, (size > 0 ? Double(used) / Double(size) * 100 : 0))
    }
}

public struct NodeMounts: Decodable, Equatable, Sendable {
    public let mounts: [MountInfo]

    public init(mounts: [MountInfo]) { self.mounts = mounts }

    private enum CodingKeys: String, CodingKey { case mounts }

    // Go encodes an empty (nil) slice as null.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        mounts = try c.field(.mounts, [])
    }
}

/// Color of a usage bar: orange from 80 %, red from 90 % (same thresholds as Android).
public enum UsageLevel: Equatable, Sendable {
    case normal, warning, critical
}

public let usageWarningPercent = 80.0
public let usageCriticalPercent = 90.0

public func usageLevel(percent: Double) -> UsageLevel {
    if percent >= usageCriticalPercent { return .critical }
    if percent >= usageWarningPercent { return .warning }
    return .normal
}

/// A used percentage as a bar fraction in [0, 1] (NaN counts as 0).
public func usageFraction(percent: Double) -> Double {
    percent.isNaN ? 0 : min(max(percent / 100, 0), 1)
}

public extension MountInfo {
    /// A disk-backed filesystem of the node itself: a device with a size, not one of the
    /// per-pod kubelet mounts (a node reports hundreds of those) nor a pseudo filesystem.
    var isPrimary: Bool {
        size > 0 && filesystem.hasPrefix("/dev/") && !mountedOn.hasPrefix("/var/lib/kubelet/")
    }
}

/// The mounts to list: the primary ones, or every mount with `showAll`; fullest first.
public func listedMounts(_ mounts: [MountInfo], showAll: Bool) -> [MountInfo] {
    sortMounts(showAll ? mounts : mounts.filter(\.isPrimary))
}

/// Fullest first, then by mount point.
public func sortMounts(_ mounts: [MountInfo]) -> [MountInfo] {
    mounts.sorted { a, b in
        if a.usedPercent != b.usedPercent { return a.usedPercent > b.usedPercent }
        return a.mountedOn < b.mountedOn
    }
}

/// A Talos volume (NodeVolumes; Talos v1.8+).
public struct VolumeInfo: Decodable, Equatable, Identifiable, Sendable {
    public let id: String
    public let phase: String
    public let type: String
    public let location: String
    /// Bytes, 0 when unknown.
    public let size: UInt64
    public let filesystem: String
    public let encryption: String
    public let mountedOn: String
    /// Why the volume is not ready, "" when it has no error.
    public let error: String

    public init(id: String, phase: String = "", type: String = "", location: String = "", size: UInt64 = 0,
                filesystem: String = "", encryption: String = "", mountedOn: String = "", error: String = "") {
        self.error = error
        self.id = id
        self.phase = phase
        self.type = type
        self.location = location
        self.size = size
        self.filesystem = filesystem
        self.encryption = encryption
        self.mountedOn = mountedOn
    }

    private enum CodingKeys: String, CodingKey { case id, phase, type, location, size, filesystem, encryption, mountedOn, error }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        phase = try c.field(.phase, "")
        type = try c.field(.type, "")
        location = try c.field(.location, "")
        size = try c.field(.size, 0)
        filesystem = try c.field(.filesystem, "")
        encryption = try c.field(.encryption, "")
        mountedOn = try c.field(.mountedOn, "")
        error = try c.field(.error, "")
    }

    /// "ready" is the only phase of a usable volume.
    public var isReady: Bool { phase.lowercased() == "ready" }
}

public struct NodeVolumes: Decodable, Equatable, Sendable {
    /// False on a Talos without volume resources.
    public let supported: Bool
    public let reason: String
    public let volumes: [VolumeInfo]

    public init(supported: Bool, reason: String = "", volumes: [VolumeInfo] = []) {
        self.supported = supported
        self.reason = reason
        self.volumes = volumes
    }

    private enum CodingKeys: String, CodingKey { case supported, reason, volumes }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        supported = try c.field(.supported, true)
        reason = try c.field(.reason, "")
        volumes = try c.field(.volumes, [])
    }
}

/// A file or directory with its size (NodeDiskUsage, like `talosctl usage`).
public struct DiskUsageEntry: Decodable, Equatable, Identifiable, Sendable {
    public let path: String
    /// Bytes.
    public let size: Int64
    public let isDir: Bool
    /// Why the size could not be read, "" when it was.
    public let error: String

    public var id: String { path }

    public init(path: String, size: Int64 = 0, isDir: Bool = false, error: String = "") {
        self.path = path
        self.size = size
        self.isDir = isDir
        self.error = error
    }

    private enum CodingKeys: String, CodingKey { case path, size, isDir, error }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        path = try c.field(.path, "")
        size = try c.field(.size, 0)
        isDir = try c.field(.isDir, false)
        error = try c.field(.error, "")
    }
}

public struct DiskUsage: Decodable, Equatable, Sendable {
    public let entries: [DiskUsageEntry]
    /// Go stopped at its entry limit: the smallest were dropped.
    public let truncated: Bool

    public init(entries: [DiskUsageEntry], truncated: Bool = false) {
        self.entries = entries
        self.truncated = truncated
    }

    private enum CodingKeys: String, CodingKey { case entries, truncated }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        entries = try c.field(.entries, [])
        truncated = try c.field(.truncated, false)
    }
}

/// Where the explorer can start. The node walks the whole tree under a path whatever the
/// depth, so nothing is measured until the user picks one: the quick ones first, the large
/// ones ("/var" of a busy node can take minutes, "/" longer) last.
public let diskUsageShortcuts = ["/var/log", "/var/lib/etcd", "/etc", "/opt", "/system/state", "/var/lib", "/var", "/"]

/// "var/lib//" → "/var/lib"; "" → "/".
public func normalizedPath(_ path: String) -> String {
    let parts = path.split(separator: "/", omittingEmptySubsequences: true)
    return "/" + parts.joined(separator: "/")
}

/// One step of the path bar: what to show and where it leads.
public struct PathCrumb: Equatable, Identifiable, Sendable {
    public let name: String
    public let path: String

    public var id: String { path }

    public init(name: String, path: String) {
        self.name = name
        self.path = path
    }
}

/// "/var/lib" → "/", "var", "lib" with their full paths; always starts at the root.
public func pathBreadcrumb(_ path: String) -> [PathCrumb] {
    var crumbs = [PathCrumb(name: "/", path: "/")]
    var current = ""
    for part in path.split(separator: "/", omittingEmptySubsequences: true) {
        current += "/" + part
        crumbs.append(PathCrumb(name: String(part), path: current))
    }
    return crumbs
}

/// One explorer row: `name` is relative to the listed directory, `fraction` the share of the
/// largest row.
public struct DiskUsageRow: Equatable, Identifiable, Sendable {
    public let path: String
    public let name: String
    public let size: Int64
    public let isDir: Bool
    public let fraction: Double
    public let error: String

    public var id: String { path }

    public init(path: String, name: String, size: Int64, isDir: Bool, fraction: Double, error: String = "") {
        self.error = error
        self.path = path
        self.name = name
        self.size = size
        self.isDir = isDir
        self.fraction = fraction
    }
}

/// What is under `root` among `entries` (which may include `root` itself, with the total),
/// biggest first then by path. Same rule as Android.
public func diskUsageRows(_ entries: [DiskUsageEntry], root: String) -> [DiskUsageRow] {
    let base = normalizedPath(root)
    var seen = Set<String>()
    var children: [DiskUsageEntry] = []
    for entry in entries {
        let path = normalizedPath(entry.path)
        if path != base, seen.insert(path).inserted {
            children.append(DiskUsageEntry(path: path, size: entry.size, isDir: entry.isDir, error: entry.error))
        }
    }
    let largest = max(children.map(\.size).max() ?? 1, 1)
    return children
        .sorted { a, b in
            if a.size != b.size { return a.size > b.size }
            return a.path < b.path
        }
        .map { entry in
            let name = base != "/" && entry.path.hasPrefix(base + "/")
                ? String(entry.path.dropFirst(base.count + 1)) : String(entry.path.dropFirst())
            return DiskUsageRow(path: entry.path, name: name, size: entry.size, isDir: entry.isDir,
                                fraction: min(max(Double(entry.size) / Double(largest), 0), 1), error: entry.error)
        }
}

/// Size of `root` itself when `entries` reports it, else the sum of its direct children.
public func diskUsageTotal(_ entries: [DiskUsageEntry], root: String) -> Int64 {
    let base = normalizedPath(root)
    if let own = entries.first(where: { normalizedPath($0.path) == base }) { return own.size }
    return diskUsageRows(entries, root: root).filter { !$0.name.contains("/") }.reduce(Int64(0)) { $0 &+ $1.size }
}

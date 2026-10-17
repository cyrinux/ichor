import Foundation

// Background alerts on node storage for a cluster added from a kubeconfig (CYR-335): the Go
// core's KubeNodeDisk (each node's nodefs from the kubelet summary, and its DiskPressure), turned
// into the same storage issues the Talos path builds (StorageAlerts.swift), so the alerts, their
// wording and the trend are shared. Same as Android's KubeStorageIssues.kt.

/// Each node's disk as its kubelet reports it (the Go core's KubeNodeDisk).
public struct KubeNodeDisk: Decodable, Equatable, Sendable {
    /// The nodes whose kubelet answered; one that did not is left out (never reported as full).
    public let nodes: [KubeNodeDiskNode]
    /// A kubelet read (or the node list) was refused: a missing reading, never an alert.
    public let forbidden: Bool
    /// How many kubelets answered.
    public let measured: Int
    /// Too many nodes to ask each kubelet on a background run: not read.
    public let skipped: Bool

    public init(nodes: [KubeNodeDiskNode] = [], forbidden: Bool = false, measured: Int = 0, skipped: Bool = false) {
        self.nodes = nodes
        self.forbidden = forbidden
        self.measured = measured
        self.skipped = skipped
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        nodes = try c.field(.nodes, [])
        forbidden = try c.field(.forbidden, false)
        measured = try c.field(.measured, 0)
        skipped = try c.field(.skipped, false)
    }

    private enum CodingKeys: String, CodingKey { case nodes, forbidden, measured, skipped }
}

/// One node's nodefs (the kubelet summary's `node.fs`) and imagefs fill, and its DiskPressure condition.
public struct KubeNodeDiskNode: Decodable, Equatable, Sendable {
    public let name: String
    public let usedPercent: Int
    public let availableBytes: Int64
    public let capacityBytes: Int64
    public let imageFsUsedPercent: Int
    public let diskPressure: Bool

    public init(name: String, usedPercent: Int, availableBytes: Int64 = 0, capacityBytes: Int64 = 0,
                imageFsUsedPercent: Int = 0, diskPressure: Bool = false) {
        self.name = name
        self.usedPercent = usedPercent
        self.availableBytes = availableBytes
        self.capacityBytes = capacityBytes
        self.imageFsUsedPercent = imageFsUsedPercent
        self.diskPressure = diskPressure
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        usedPercent = try c.field(.usedPercent, 0)
        availableBytes = try c.field(.availableBytes, 0)
        capacityBytes = try c.field(.capacityBytes, 0)
        imageFsUsedPercent = try c.field(.imageFsUsedPercent, 0)
        diskPressure = try c.field(.diskPressure, false)
    }

    /// The kubelet reported a size: its percent means something.
    public var measured: Bool { capacityBytes > 0 }

    private enum CodingKeys: String, CodingKey { case name, usedPercent, availableBytes, capacityBytes, imageFsUsedPercent, diskPressure }
}

/// The volume name a kube node's storage issue carries: the kubelet's root filesystem.
public let kubeNodeFsVolume = "nodefs"

/// The storage issue key of a kube node: "<node>|nodefs", as the Talos path keys a volume ("<node>|<volume>").
public func kubeNodeDiskKey(_ node: String) -> String { "\(node)|\(kubeNodeFsVolume)" }

/// Why a node's disk is critical whatever its fill (the issue's detail).
let kubeDiskPressure = "DiskPressure"

/// The nodes worth judging: named, and measured by their kubelet or reporting DiskPressure.
private func judgedNodes(_ disk: KubeNodeDisk) -> [KubeNodeDiskNode] {
    disk.nodes.filter { !$0.name.isEmpty && ($0.measured || $0.diskPressure) }
}

/// The storage issues of a cluster added from a kubeconfig, in the Talos path's format
/// (StorageIssue, kind fill, host the node's name, volume "nodefs", see storageIssuesOf): used at
/// `warn` % or more is a warning, at `critical` % or more critical, and a node reporting
/// DiskPressure is critical whatever its fill. Nil when nothing could be read (the read skipped
/// on a large cluster, nodes/proxy refused, no kubelet answered): an absent reading, never an
/// alert nor an all-clear. A node of `listed` (this check's node list) that did not answer keeps
/// its `known` issues; one no longer listed drops them. A demo cluster's disks never open an
/// issue (a demo never notifies). Same as Android's kubeStorageIssuesOf.
public func storageIssues(of disk: KubeNodeDisk, warn: Int, critical: Int, listed: Set<String> = [],
                          known: [String: String] = [:], demo: Bool = false) -> [String: String]? {
    if demo { return [:] }
    if disk.skipped { return nil }
    let judged = judgedNodes(disk)
    if judged.isEmpty && (disk.forbidden || disk.measured == 0) { return nil }
    let warnAt = min(max(warn, storageWarnRange.lowerBound), storageWarnRange.upperBound)
    let range = storageCritRange(warn: warnAt)
    let criticalAt = min(max(critical, range.lowerBound), range.upperBound)
    let answered = Set(judged.map(\.name))
    var out = known.filter { key, _ in
        let node = storageIssueNode(key)
        return listed.contains(node) && !answered.contains(node)
    }
    for node in judged {
        let severity: String
        if node.diskPressure || node.usedPercent >= criticalAt {
            severity = dataCritical
        } else if node.usedPercent >= warnAt {
            severity = dataWarning
        } else {
            continue
        }
        out[kubeNodeDiskKey(node.name)] = StorageIssue(
            severity: severity, kind: StorageIssue.fill, hostname: node.name, name: kubeNodeFsVolume,
            percent: String(min(max(node.usedPercent, 0), 100)),
            freeBytes: UInt64(max(0, node.availableBytes)), sizeBytes: UInt64(max(0, node.capacityBytes)),
            detail: node.diskPressure ? kubeDiskPressure : "").value
    }
    return out
}

/// Every measured node's nodefs fill for the history ring and the storage trend, keyed like its
/// issues; empty when the read was skipped.
public func historyVolumes(_ disk: KubeNodeDisk) -> [HistoryVolumeRecord] {
    guard !disk.skipped else { return [] }
    return disk.nodes.filter { !$0.name.isEmpty && $0.measured }.map {
        HistoryVolumeRecord(key: kubeNodeDiskKey($0.name), name: kubeNodeFsVolume, node: $0.name, usedPercent: Double($0.usedPercent))
    }
}

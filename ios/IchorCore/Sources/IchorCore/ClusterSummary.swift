import Foundation

/// The cluster as a whole: every node ready, some trouble, or nothing answering.
public enum ClusterStatus: String, Sendable {
    case healthy, degraded, down
}

/// Headline numbers of the overview's summary; capacity only counts the nodes that answered.
/// Same as Android's model/ClusterSummary.kt.
public struct ClusterSummary: Equatable, Sendable {
    public let total: Int
    public let ready: Int
    public let notReady: Int
    public let unreachable: Int
    /// Distinct Talos versions, oldest first: more than one means an upgrade in progress.
    public let versions: [String]
    public let cpuCount: Int
    public let memTotal: UInt64
    public let memAvailable: UInt64

    public init(total: Int, ready: Int, notReady: Int, unreachable: Int, versions: [String],
                cpuCount: Int, memTotal: UInt64, memAvailable: UInt64) {
        self.total = total
        self.ready = ready
        self.notReady = notReady
        self.unreachable = unreachable
        self.versions = versions
        self.cpuCount = cpuCount
        self.memTotal = memTotal
        self.memAvailable = memAvailable
    }

    public init(nodes: [NodeOverview]) {
        let reachable = nodes.filter(\.reachable)
        var versions: [String] = []
        for version in reachable.map(\.version) where !version.trimmingCharacters(in: .whitespaces).isEmpty
            && !versions.contains(version) {
            versions.append(version)
        }
        self.init(
            total: nodes.count,
            ready: nodes.filter { $0.health == .ready }.count,
            notReady: nodes.filter { $0.health == .notReady }.count,
            unreachable: nodes.filter { $0.health == .unreachable }.count,
            versions: versions.sorted { compareTalosVersions($0, $1) < 0 },
            cpuCount: reachable.reduce(0) { $0 + $1.cpuCount },
            memTotal: reachable.reduce(0) { $0 + $1.memTotal },
            memAvailable: reachable.reduce(0) { $0 + $1.memAvailable }
        )
    }

    public var status: ClusterStatus {
        if total == 0 || unreachable == total { return .down }
        return ready == total ? .healthy : .degraded
    }

    /// Memory in use across the cluster, nil when no node said how much it has.
    public var memUsedFraction: Double? {
        memTotal == 0 ? nil : usedFraction(total: memTotal, available: memAvailable)
    }

    /// The version span to show: "v1.11.2", "v1.10.4 – v1.11.2" while the nodes differ, nil
    /// when none said.
    public var versionSpan: String? {
        guard let first = versions.first, let last = versions.last else { return nil }
        return versions.count > 1
            ? "\(displayTalosVersion(first)) – \(displayTalosVersion(last))" : displayTalosVersion(first)
    }
}

import Foundation

/// One piece of what Siri says about a cluster ("Cluster status" intent): the app turns each into
/// words in the user's language and joins them, the check's age last.
public enum ClusterStatusPart: Equatable, Sendable {
    case nodes(Int)
    case allReady
    case notReady(Int)
    case unreachable(Int)
    case etcdHealthy
    case etcdAlarms(Int)
    /// The snapshot is older than glanceStaleAfter: what it says may no longer be true.
    case stale
}

/// What the last background check of a cluster says, from the snapshot the widget shows (no
/// network, no credentials): the parts in order and when the check ran; nil without a snapshot.
public struct ClusterStatusSummary: Equatable, Sendable {
    public let parts: [ClusterStatusPart]
    public let checkedAt: Date

    public init(parts: [ClusterStatusPart], checkedAt: Date) {
        self.parts = parts
        self.checkedAt = checkedAt
    }

    public var stale: Bool { parts.contains(.stale) }

    /// A problem among the parts: the snippet's tone.
    public var healthy: Bool {
        !parts.contains { part in
            switch part {
            case .notReady, .unreachable, .etcdAlarms: return true
            default: return false
            }
        }
    }
}

/// The widget's glance rules (glanceItems, isGlanceStale) as parts: the node count, then node
/// problems or "all ready", then etcd when it was checked, then "stale" when it is old.
public func clusterStatusSummary(_ snapshot: ClusterSnapshot?, now: Date) -> ClusterStatusSummary? {
    guard let snapshot else { return nil }
    var parts: [ClusterStatusPart] = [.nodes(snapshot.nodes.count)]
    if snapshot.notReadyCount > 0 { parts.append(.notReady(snapshot.notReadyCount)) }
    if snapshot.unreachableCount > 0 { parts.append(.unreachable(snapshot.unreachableCount)) }
    if snapshot.notReadyCount == 0 && snapshot.unreachableCount == 0 { parts.append(.allReady) }
    if snapshot.etcdChecked {
        parts.append(snapshot.etcdAlarms.isEmpty ? .etcdHealthy : .etcdAlarms(snapshot.etcdAlarms.count))
    }
    if isGlanceStale(snapshot, now: now) { parts.append(.stale) }
    return ClusterStatusSummary(parts: parts, checkedAt: snapshot.takenAt)
}

/// What "Open cluster" and "Run a checkup" (from Shortcuts, Siri or Control Center) do, set by the
/// app at launch: the intents are compiled into the widget too (its controls), but they run in the
/// app's process, where the share links are opened. `cluster` is a monitor key (WidgetCluster.id);
/// nil for the active cluster.
@MainActor
public enum ClusterScreenHandler {
    public static var open: ((_ cluster: String?, _ screen: ShareTarget.Target) async -> Void)?
}

/// The cluster an intent acts on: the one chosen if the app still lists it, else the active one.
public func intentCluster(_ chosen: String?, clusters: [WidgetCluster], active: String?) -> WidgetCluster? {
    if let chosen, let found = clusters.first(where: { $0.id == chosen }) { return found }
    return active.flatMap { id in clusters.first { $0.id == id } }
}

import Foundation

// Mirrors go/ichorgo/kube_longhorn_actions.go: which actions a volume or node offers.

/// What KubeLonghornAction runs; the raw value is the action name the Go core takes.
public enum LonghornAction: String, CaseIterable, Sendable {
    /// Volume: snapshot it and back the snapshot up to its target.
    case backup
    /// Volume: give the space freed in its filesystem back to Longhorn.
    case trim
    /// Volume: set its replica count (the action's value).
    case replicas
    /// Node: accept new replicas again; also ends an eviction.
    case schedulingOn
    /// Node: no new replicas, the existing ones stay.
    case schedulingOff
    /// Node: no new replicas, every replica moved to other nodes.
    case evict
    /// Node: stop moving replicas away; scheduling stays off.
    case cancelEviction
}

/// The replica counts Longhorn accepts.
public enum LonghornReplicas {
    public static let min = 1
    public static let max = 20
}

extension LonghornVolume {
    /// Backup and trim need the volume attached (an engine running); the replica count can
    /// always change. None without Longhorn's namespace (an older core did not send it).
    public var actions: [LonghornAction] {
        guard !namespace.isEmpty else { return [] }
        return state == "attached" ? [.backup, .trim, .replicas] : [.replicas]
    }

    /// The highest replica count to offer: one per node, at least the current count, at most
    /// what Longhorn accepts.
    public func maxReplicas(nodes: Int) -> Int {
        Swift.min(LonghornReplicas.max, Swift.max(nodes, replicasDesired, LonghornReplicas.min))
    }

    /// Where the replica picker starts: the current count, within what it offers.
    public func defaultReplicas(nodes: Int) -> Int {
        Swift.min(Swift.max(replicasDesired, LonghornReplicas.min), maxReplicas(nodes: nodes))
    }
}

extension LonghornNode {
    /// Scheduling off or on (whichever it is not), then evict or cancel the eviction. None
    /// without Longhorn's namespace (an older core did not send it).
    public var actions: [LonghornAction] {
        guard !namespace.isEmpty else { return [] }
        return [allowScheduling ? .schedulingOff : .schedulingOn, evictionRequested ? .cancelEviction : .evict]
    }
}

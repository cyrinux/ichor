import Foundation

// The same rules as Android's TalosRollout.kt.

/// Where a node stands in the rolling upgrade to the latest release.
public enum RolloutState: Equatable, Sendable {
    case pending, upgrading, waitingHealthy, done, failed
}

public struct RolloutRow: Equatable, Identifiable, Sendable {
    public let node: NodeOverview
    public let state: RolloutState

    public var id: String { node.node }
    public var done: Bool { state == .done }
}

/// The upgrade the app follows, as the rollout needs it (`waiting`: the node rebooted and is
/// coming back).
public struct RolloutRun: Equatable, Sendable {
    public let node: String
    public let waiting: Bool
    public let finished: Bool
    public let failed: Bool

    public init(node: String, waiting: Bool = false, finished: Bool = false, failed: Bool = false) {
        self.node = node
        self.waiting = waiting
        self.finished = finished
        self.failed = failed
    }
}

/// etcd members answering without errors, out of all of them.
public struct EtcdHealth: Equatable, Sendable {
    public let healthy: Int
    public let members: Int

    public init(healthy: Int, members: Int) {
        self.healthy = healthy
        self.members = members
    }

    public var degraded: Bool { healthy < members }
}

/// Why no other node may be upgraded now.
public enum RolloutHold: Equatable, Sendable {
    /// One at a time: the node is being upgraded, or (`waiting`) is coming back from it.
    case upgrading(NodeOverview, waiting: Bool)
    /// These nodes do not answer or are not ready.
    case unhealthy([NodeOverview])
    case etcd(EtcdHealth)
}

/// The rolling upgrade to the latest release as a plan: every node under its role in upgrade
/// order (control plane first, done ones last), what holds it up, and the node to do next.
public struct TalosRollout: Equatable, Sendable {
    public let controlPlane: [RolloutRow]
    public let workers: [RolloutRow]
    public let hold: RolloutHold?
    /// Shown next to an upgrade in progress; nil when etcd could not be read.
    public let etcd: EtcdHealth?
    /// The node whose upgrade the app follows (its row opens the progress).
    public let followed: String?

    /// `run` is the upgrade the app follows, if any; `etcd` the members' health when it could
    /// be read. A node on `latest` that is not healthy yet still waits.
    public init(nodes: [NodeOverview], latest: String, run: RolloutRun? = nil, etcd: EtcdHealth? = nil) {
        func state(_ node: NodeOverview) -> RolloutState {
            let own = run.flatMap { $0.node == node.node ? $0 : nil }
            if let own, !own.finished { return own.waiting ? .waitingHealthy : .upgrading }
            if talosVersionParts(node.version) != nil, !isOutdatedTalos(node.version, latest: latest) {
                return node.health == .ready ? .done : .waitingHealthy
            }
            // Upgraded, but the overview still shows what the node ran before: not pending again.
            if let own { return own.failed ? .failed : .waitingHealthy }
            return .pending
        }
        // Done last; else the oldest version first (unknown ones last), then by hostname.
        let rows = nodes.map { RolloutRow(node: $0, state: state($0)) }.sorted { a, b in
            if a.done != b.done { return !a.done }
            let (va, vb) = (talosVersionParts(a.node.version), talosVersionParts(b.node.version))
            if va != vb {
                guard let va else { return false }
                guard let vb else { return true }
                return va.lexicographicallyPrecedes(vb)
            }
            return a.node.hostname < b.node.hostname
        }
        controlPlane = rows.filter { $0.node.role == "controlplane" }
        workers = rows.filter { $0.node.role != "controlplane" }
        // The followed node until it is done, or failed: also while the overview lags behind the run.
        let active = rows.first { $0.node.node == run?.node && ($0.state == .upgrading || $0.state == .waitingHealthy) }
        let unhealthy = rows.map(\.node).filter { $0.health != .ready }
        if let active {
            hold = .upgrading(active.node, waiting: active.state == .waitingHealthy)
        } else if !unhealthy.isEmpty {
            hold = .unhealthy(unhealthy)
        } else if let etcd, etcd.degraded {
            hold = .etcd(etcd)
        } else {
            hold = nil
        }
        self.etcd = etcd
        followed = run.flatMap { run in rows.contains { $0.node.node == run.node } ? run.node : nil }
    }

    public var controlPlaneDone: Int { controlPlane.filter(\.done).count }
    public var workersDone: Int { workers.filter(\.done).count }

    /// Workers are better left for later: the control plane is not fully upgraded.
    public var workersWait: Bool { controlPlane.contains { !$0.done } }

    /// What "Upgrade next" picks: control plane first; in a role, a failed node to retry, else
    /// the first pending.
    public var next: RolloutRow? {
        for rows in [controlPlane, workers] {
            if let row = rows.first(where: { $0.state == .failed }) ?? rows.first(where: { $0.state == .pending }) {
                return row
            }
        }
        return nil
    }

    /// Whether the row opens its upgrade screen: the followed node always (its progress); a
    /// pending or failed one only when nothing else holds the rollout up. A node that is itself
    /// the only unhealthy one stays open, since upgrading it takes no second node down.
    public func canOpen(_ row: RolloutRow) -> Bool {
        switch row.state {
        case .done:
            return false
        case .upgrading, .waitingHealthy:
            return row.node.node == followed
        case .pending, .failed:
            guard row.node.reachable else { return false }
            switch hold {
            case nil: return true
            case .unhealthy(let nodes)?: return nodes.allSatisfy { $0.node == row.node.node }
            default: return false
            }
        }
    }

    /// Upgrading the row goes against the order (a worker before the control plane is done):
    /// to confirm first.
    public func needsConfirm(_ row: RolloutRow) -> Bool {
        workersWait && workers.contains(row) && (row.state == .pending || row.state == .failed)
    }
}

/// The node rebooted: the followed upgrade now waits for it to come back.
public func upgradeWaitsForNode(_ events: [UpgradeProgress]) -> Bool {
    events.contains { event in UpgradePhase(go: event.phase).map { $0 >= .waiting } ?? false }
}

public extension EtcdOverview {
    /// The members answering without errors; nil when etcd could not be read.
    var health: EtcdHealth? {
        guard error == nil, !members.isEmpty else { return nil }
        let answering = Set(statuses.filter { $0.error == nil && $0.errors.isEmpty }.map(\.memberId))
        return EtcdHealth(healthy: members.filter { answering.contains($0.id) }.count, members: members.count)
    }
}

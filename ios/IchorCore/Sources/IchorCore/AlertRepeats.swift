import Foundation

// Repeat until acknowledged (same as Android's monitor/AlertRepeats.kt): a critical alert that
// still fires is posted again every repeatMinutes of its rule (Go decides, AlertRulesApply)
// until it is acknowledged from its notification, snoozed, or it recovers. The snapshot keeps
// when each firing alert was first seen and last posted; the cadence is the check's.

/// The problem alerts `snapshot` still holds, as a check would post them now: its nodes not
/// ready, its etcd alarms and the issues of its tracks already notified. Built by evaluating it
/// against the same snapshot all clear, so each alert has its usual key and text. The
/// certificate (warned daily already) and the volume trends are not among them, nor anything of
/// a cluster where no node answered.
public func firingAlerts(_ snapshot: ClusterSnapshot, now: Date) -> [Alert] {
    var clear = snapshot
    clear.nodes = snapshot.nodes.mapValues { NodeState(hostname: $0.hostname, health: .ready) }
    clear.etcdAlarms = []
    clear.lastCertWarnDay = Int64((now.timeIntervalSince1970 / 86_400).rounded(.down))
    func cleared(_ track: IssueTrack) -> IssueTrack {
        // Pending: a warning alerts at once, as on its second check.
        IssueTrack(watched: track.watched, checked: track.checked, issues: [:], pending: Array(track.issues.keys))
    }
    clear.dataTrack = cleared(snapshot.dataTrack)
    clear.gitopsTrack = cleared(snapshot.gitopsTrack)
    clear.checkupTrack = cleared(snapshot.checkupTrack)
    clear.amTrack = cleared(snapshot.amTrack)
    clear.storageTrack = cleared(snapshot.storageTrack)
    clear.sensorTrack = cleared(snapshot.sensorTrack)
    clear.pluginTrack = cleared(snapshot.pluginTrack)
    return evaluate(previous: clear, current: snapshot, now: now).alerts.filter(\.problem)
}

/// `next` with the first-seen and last-posted times of its firing alerts carried over from
/// `previous`, the cluster's snapshot before the check: a newly firing one is first seen `now`,
/// one that no longer fires is forgotten (it repeats again only once posted again). Another
/// context's snapshot is a baseline.
public func withAlertTimes(previous: ClusterSnapshot?, next: ClusterSnapshot, now: Date) -> ClusterSnapshot {
    let known = previous.flatMap { $0.context == next.context ? $0 : nil }
    let firing = Set(firingAlerts(next, now: now).map(\.key))
    var out = next
    out.firstSeen = Dictionary(uniqueKeysWithValues: firing.map { ($0, known?.firstSeen[$0] ?? now.epochMillis) })
    out.lastNotified = (known?.lastNotified ?? [:]).filter { firing.contains($0.key) }
    return out
}

/// The alerts of `snapshot` (a cluster read this run) that still fire, unchanged, since they were
/// posted: what Go may post again. Not the `changed` ones (keys), posted as they are now.
public func ongoingAlerts(_ snapshot: ClusterSnapshot, changed: Set<String>, now: Date) -> [Alert] {
    guard !snapshot.unreachableAsAWhole else { return [] }
    return firingAlerts(snapshot, now: now).filter { snapshot.lastNotified[$0.key] != nil && !changed.contains($0.key) }
}

/// `snapshot` with `keys` posted at `now`: what their next repeat counts from. Only the ones
/// still firing.
public func withNotified(_ snapshot: ClusterSnapshot, keys: [String], now: Date) -> ClusterSnapshot {
    var out = snapshot
    for key in keys where snapshot.firstSeen[key] != nil { out.lastNotified[key] = now.epochMillis }
    return out
}

/// Whole minutes from `firstSeen` (unix ms) to `now`, at least 1.
public func stillFiringMinutes(firstSeen: Int64, now: Date) -> Int {
    guard firstSeen > 0 else { return 1 }
    return max(1, Int((now.epochMillis - firstSeen) / 60_000))
}

/// The alerts acknowledged from their notification, per cluster (its fingerprint) and alert
/// key, when (unix ms): an acknowledged alert is not posted again by its repeat. It ends when the
/// alert stops firing (keepingFiring); kept on this device only.
public struct AlertAcks: Codable, Equatable, Sendable {
    public private(set) var at: [String: [String: Int64]]

    public init(at: [String: [String: Int64]] = [:]) {
        self.at = at
    }

    public func acknowledging(cluster: String, key: String, now: Date) -> AlertAcks {
        guard !cluster.isEmpty, !key.isEmpty else { return self }
        var next = at
        next[cluster, default: [:]][key] = now.epochMillis
        return AlertAcks(at: next)
    }

    public func isAcknowledged(cluster: String, key: String) -> Bool { at[cluster]?[key] != nil }

    /// Without `cluster`'s acknowledgements whose alert is not among `firing` (keys): it recovered.
    public func keepingFiring(cluster: String, firing: Set<String>) -> AlertAcks {
        guard let acks = at[cluster] else { return self }
        var next = at
        let kept = acks.filter { firing.contains($0.key) }
        next[cluster] = kept.isEmpty ? nil : kept
        return AlertAcks(at: next)
    }

    /// Only the acknowledgements of `clusters` (fingerprints): a removed cluster's go with it.
    public func keeping(clusters: [String]) -> AlertAcks {
        let kept = Set(clusters)
        return AlertAcks(at: at.filter { kept.contains($0.key) })
    }
}

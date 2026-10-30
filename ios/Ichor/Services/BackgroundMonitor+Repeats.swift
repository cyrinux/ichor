import Foundation
import IchorCore

// Repeat until acknowledged (same as Android's MonitorWorker.postAll): the critical alerts that
// still fire since they were posted are sent to Go with their times, and posted again when it
// says their repeat is due (postAll). iOS cannot schedule the repeat itself: it waits for the
// next background refresh, 15 minutes at the shortest.
extension BackgroundMonitor {
    /// The ongoing alerts of the clusters read this run (`jobs`, where a node answered), as
    /// notifications Go may post again: not the ones `changed` already posts, not one never
    /// posted. The acknowledgements of an alert no longer firing (or of a removed cluster, not in
    /// `clusters`) end here.
    static func pendingRepeats(
        _ jobs: [ClusterJob], changed: [ClusterAlerts], snapshots: [String: ClusterSnapshot], clusters: [String],
        labels: ClusterLabels, snoozes: AlertSnoozes, now: Date
    ) async -> [PendingAlert] {
        var firing: [String: Set<String>] = [:]
        var ongoing: [ClusterAlerts] = []
        for job in jobs {
            guard let snapshot = snapshots[job.key] else { continue }
            firing[job.context.fingerprint] = Set(firingAlerts(snapshot, now: now).map(\.key))
            let posted = Set(changed.filter { $0.job.key == job.key }.flatMap { $0.alerts.map(\.key) })
            let alerts = ongoingAlerts(snapshot, changed: posted, now: now)
            if !alerts.isEmpty {
                ongoing.append(ClusterAlerts(job: job, alerts: alerts, snapshot: snapshot, previous: job.previous))
            }
        }
        let acks = AlertAckStore.keep(clusters: clusters, firing: firing)
        let items = await pending(ongoing, labels: labels, snoozes: snoozes, now: now)
        return items.map { item in
            var out = item
            let snapshot = snapshots[item.clusterKey]
            out.input.ongoing = true
            out.input.firstSeenMillis = snapshot?.firstSeen[item.alert.key] ?? 0
            out.input.lastNotifiedMillis = snapshot?.lastNotified[item.alert.key] ?? 0
            out.input.acknowledged = acks.isAcknowledged(cluster: item.input.cluster, key: item.alert.key)
            return out
        }
    }
}

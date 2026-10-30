import Foundation
import IchorCore
import os
import UserNotifications

/// One notification a run is about to post, whatever told it (a check's alert, a merged pull
/// request, "sign in again"), with what the alert rules read of it (`input`).
struct PendingAlert {
    let context: ContextSummary
    /// The cluster's monitor key (monitorClusterKey): its notification ids and history.
    let clusterKey: String
    /// The cluster as the app names it (the subtitle).
    let label: String
    let alert: Alert
    var input: AlertInput
    let localized: (title: String, text: String)
    let link: URL?
    let actions: [AlertAction]
    let info: [String: String]
}

/// The alert key a cluster's digest is posted under: each run's digest replaces the previous one.
let digestAlertKey = "digest"

private let rulesLog = Logger(subsystem: "name.levis.ichor", category: "alert-rules")

// The alert rules at post time (quiet hours, minimum severity, digest; same as Android's
// MonitorWorker.postAll): decided once per run by the Go core over every cluster's alerts.
extension BackgroundMonitor {
    /// Posts a run's alerts under the alert rules: each one Go says to notify on its own, one
    /// digest per digest cluster; the ones quiet hours or the minimum severity muted are
    /// recorded for "Since you last looked", the snoozed ones dropped. Without rules, or when Go
    /// refuses them, every alert not snoozed is posted as before (alertRulePlan): a rules error
    /// never drops an alert. An ongoing one (pendingRepeats) is posted again, "Still firing ·
    /// 25 min", only when Go says its repeat is due; the posted ones that repeat get Acknowledge.
    /// Returns the problems posted, by cluster key: what their next repeat counts from.
    @discardableResult
    static func postAll(_ pending: [PendingAlert], now: Date) async -> [String: [String]] {
        guard !pending.isEmpty else { return [:] }
        let hide = UserDefaults.standard.bool(forKey: "appLockEnabled")
        let millis = Int64((now.timeIntervalSince1970 * 1000).rounded())
        let inputs = pending.map(\.input)
        let rules = AlertRulesStore.load()
        var decisions: AlertDecisions?
        if rules != .none {
            do {
                decisions = try await TalosClient.decideAlerts(rules: rules, alerts: inputs, now: now)
            } catch {
                rulesLog.warning("alert rules not applied, posting every alert: \(error.localizedDescription)")
            }
        }
        let plan = alertRulePlan(inputs: inputs, decisions: decisions)
        var mutes: [(cluster: String, key: String, mute: AlertMute)] = []
        var digested: [String: PendingAlert] = [:]
        var posted: [String: [String]] = [:]
        for (index, (item, outcome)) in zip(pending, plan.outcomes).enumerated() {
            let actions = plan.repeats[index] ? withAcknowledge(item.actions) : item.actions
            let id = alertNotificationID(cluster: item.clusterKey, alertKey: item.alert.key)
            switch outcome {
            case .notify:
                await post(
                    item.alert, localized: item.localized, cluster: item.label, id: id, link: item.link,
                    actions: actions, info: item.info, hideDetails: hide)
                if item.alert.problem { posted[item.clusterKey, default: []].append(item.alert.key) }
            case .repeat:
                let minutes = stillFiringMinutes(firstSeen: item.input.firstSeenMillis, now: now)
                let text = String(localized: "Still firing · \(minutes) min · \(item.localized.text)")
                await post(
                    item.alert, localized: (item.localized.title, text), cluster: item.label, id: id, link: item.link,
                    actions: actions, info: item.info, hideDetails: hide, timeSensitive: true)
                posted[item.clusterKey, default: []].append(item.alert.key)
            case .mute(let reason, let until):
                mutes.append((item.clusterKey, item.alert.key, AlertMute(reason: reason, until: until, at: millis)))
            case .digest:
                if digested[item.input.cluster] == nil { digested[item.input.cluster] = item }
            case .drop:
                continue
            }
        }
        AlertMuteStore.record(mutes, now: millis)
        for digest in plan.digests {
            guard let first = digested[digest.cluster] else { continue }
            await postDigest(digest, first: first, hideDetails: hide)
        }
        return posted
    }

    /// One notification for a digest cluster's alerts of a run: "3 findings on staging-eu" and
    /// "Critical: 1, warnings: 2", in the category of its first alert, opening the cluster's
    /// home. Posted under the same id each run, so it replaces the previous run's.
    private static func postDigest(_ digest: AlertDigest, first: PendingAlert, hideDetails: Bool) async {
        let content = UNMutableNotificationContent()
        content.title = String(localized: "\(digest.count) findings on \(first.label)")
        content.subtitle = first.label
        content.body = String(localized: "Critical: \(digest.critical), warnings: \(digest.warning)")
        content.sound = .default
        content.categoryIdentifier = category(alertKey: first.alert.key, hideDetails: hideDetails)
        var info = [AlertNotificationActions.clusterKey: first.context.fingerprint]
        if !first.context.clusterID.isEmpty {
            var target = ShareTarget.screen(.cluster)
            target.cluster = first.context.clusterID
            if let link = TalosClient.shareLink(for: target) { info["link"] = link.absoluteString }
        }
        content.userInfo = info
        let id = alertNotificationID(cluster: first.clusterKey, alertKey: digestAlertKey)
        try? await UNUserNotificationCenter.current().add(
            UNNotificationRequest(identifier: id, content: content, trigger: nil))
    }
}

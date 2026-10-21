import BackgroundTasks
import Foundation
import IchorCore
import UIKit
import UserNotifications
import WidgetKit

// The alerts a check found: their text in the user's language, their actions and links, and posting them.
extension BackgroundMonitor {
    /// Posts each cluster's alerts, named after their cluster (the notification's subtitle).
    // Internal, not private: check() in BackgroundMonitor.swift posts its outcomes.
    static func post(_ outcomes: [ClusterAlerts], now: Date) async {
        let hide = UserDefaults.standard.bool(forKey: "appLockEnabled")
        let names = UserDefaults.standard.dictionary(forKey: "clusterNames") as? [String: String] ?? [:]
        let labels = ClusterLabels(names: names, masked: UserDefaults.standard.bool(forKey: PrivacyKeys.enabled))
        let snoozes = AlertSnoozeStore.current(now: now)
        for outcome in outcomes {
            let context = outcome.job.context
            let fingerprint = context.fingerprint
            // A snoozed alert posts nothing, neither the problem nor its end (its notification's Snooze).
            for alert in snoozes.notSnoozed(outcome.alerts, cluster: fingerprint, now: now) {
                let link = alertLink(alert, snapshot: outcome.snapshot, cluster: context.clusterID)
                let text = localized(alert, snapshot: outcome.snapshot, previous: outcome.previous, now: now)
                let wake = await canWake(alert, cluster: fingerprint)
                await post(alert, localized: text, cluster: labels.of(context),
                           id: alertNotificationID(cluster: outcome.job.key, alertKey: alert.key), link: link,
                           actions: alertActions(key: alert.key, problem: alert.problem, canWake: wake),
                           info: actionInfo(alert, snapshot: outcome.snapshot, cluster: fingerprint), hideDetails: hide)
            }
        }
    }

    /// Whether the node of a node alert can be woken: a Wake-on-LAN setting or a MAC seen.
    private static func canWake(_ alert: Alert, cluster: String) async -> Bool {
        guard alert.problem, !cluster.isEmpty, alert.key.hasPrefix("node:") else { return false }
        let address = String(alert.key.dropFirst("node:".count))
        return await MainActor.run {
            WakeOnLanStore.shared.loadIfNeeded()
            return !WakeOnLanStore.shared.wakeTargets(fingerprint: cluster, node: address).isEmpty
        }
    }

    /// What the notification's Snooze and Wake need: the cluster, the alert, a node's hostname.
    private static func actionInfo(_ alert: Alert, snapshot: ClusterSnapshot, cluster: String) -> [String: String] {
        var info = [AlertNotificationActions.clusterKey: cluster, AlertNotificationActions.alertKey: alert.key]
        if alert.key.hasPrefix("node:"), let state = snapshot.nodes[String(alert.key.dropFirst("node:".count))] {
            info[AlertNotificationActions.hostKey] = state.hostname
        }
        return info
    }

    /// Argo CD and Flux issues (a tool not installed reads fine and adds nothing); nil when neither

    /// IchorCore builds English alerts; this rebuilds their text in the user's
    /// language from the alert key and the snapshot (same wording, keys in Localizable.xcstrings).
    /// `previous`: the snapshot before the check, which still names an Alertmanager alert that resolved.
    static func localized(_ alert: Alert, snapshot: ClusterSnapshot, previous: ClusterSnapshot? = nil,
                          now: Date) -> (title: String, text: String) {
        let parts = alert.key.split(separator: ":", maxSplits: 1).map(String.init)
        guard let kind = parts.first else { return (alert.title, alert.text) }
        let subject = parts.count > 1 ? parts[1] : ""
        switch kind {
        case "node":
            guard let state = snapshot.nodes[subject] else { return (alert.title, alert.text) }
            let title = switch state.health {
            case .ready: String(localized: "\(state.hostname) is ready again")
            case .notReady: String(localized: "\(state.hostname) is not ready")
            case .unreachable: String(localized: "\(state.hostname) is unreachable")
            }
            return (title, alert.text)
        case "etcd":
            let alarm = subject.split(separator: ":", maxSplits: 1).map(String.init)
            let text = String(localized: "\(alarm.last ?? subject) on member \(alarm.first ?? "")")
            return (String(localized: "etcd alarm raised"), text)
        case "cert":
            let days = daysUntil(snapshot.certNotAfter, now: now)
            if snapshot.kube {
                let text = days < 0
                    ? String(localized: "The kubeconfig credentials expired \(-days) days ago.")
                    : String(localized: "The kubeconfig credentials expire in \(days) days. Import a new kubeconfig.")
                return (String(localized: "kubeconfig credentials"), text)
            }
            let text = days < 0
                ? String(localized: "The client certificate expired \(-days) days ago.")
                : String(localized: "The client certificate expires in \(days) days. Generate a new talosconfig.")
            return (String(localized: "talosconfig certificate"), text)
        case "data":
            // "system|label": the label names the volume or cluster, the system goes in the text.
            let issue = subject.split(separator: "|", maxSplits: 1).map(String.init)
            let label = issue.last ?? subject
            let system = dataSystemTitle(subject)
            guard alert.problem else { return (String(localized: "\(label) is healthy again"), system) }
            if issue.first == "snapshots" { return (String(localized: "\(label) needs attention"), String(localized: "No good snapshot in 48 hours")) }
            if issue.first == "backupjobs" { return (String(localized: "\(label) needs attention"), String(localized: "Observed consecutive failures: \(3)")) }
            let severity = snapshot.dataIssues[subject] == dataCritical ? ServiceHealth.critical.label : ServiceHealth.warning.label
            return (String(localized: "\(label) needs attention"), "\(system) · \(severity)")
        case "gitops":
            return localizedGitOps(alert, subject: subject, value: snapshot.gitopsIssues[subject])
        case "am":
            // "am:fingerprint": named from the issue kept, the current one or, once resolved, the last.
            guard let value = snapshot.amIssues[subject] ?? previous?.amIssues[subject] else { return (alert.title, alert.text) }
            let issue = AMIssue(value: value)
            guard alert.problem else { return (String(localized: "Alertmanager: \(issue.alertname) resolved"), issue.subject) }
            let severity = issue.severity == dataCritical ? AMSeverity.critical.label : AMSeverity.warning.label
            let line = issue.line(severity: severity)
            return (String(localized: "Alertmanager: \(issue.alertname)"), issue.summary.isEmpty ? line : "\(line)\n\(issue.summary)")
        case "checkup":
            // "section|kind|subject": the section names what kind of trouble, the subject what has it.
            let finding = CheckupSubject(key: subject)
            guard alert.problem else { return (CheckupText.monitorCheckupOk(finding.subject), CheckupText.checkupTitle) }
            let section = CheckupSectionID(rawValue: finding.section)?.title ?? finding.section
            let severity = snapshot.checkupIssues[subject] == dataCritical ? ServiceHealth.critical.label : ServiceHealth.warning.label
            return ("\(section): \(finding.subject)", "\(CheckupText.checkupTitle) · \(severity)")
        case "storage":
            // "<node>|<volume>:trend": named from the trend kept, the current one or, once resolved, the last.
            if let volume = storageTrendVolumeKey(subject: subject) {
                guard let value = snapshot.storageTrends[volume] ?? previous?.storageTrends[volume] else { return (alert.title, alert.text) }
                return localizedStorageTrend(alert, issue: StorageTrendIssue(value: value))
            }
            // "<node>|…": named from the issue kept, the current one or, once resolved, the last.
            guard let value = snapshot.storageIssues[subject] ?? previous?.storageIssues[subject] else { return (alert.title, alert.text) }
            return localizedStorage(alert, issue: StorageIssue(value: value), warn: snapshot.storageWarn)
        case "plugin":
            // The finding's key: worded by the plugin, already in the app's language (Go's locale).
            guard let value = snapshot.pluginIssues[alert.key] ?? previous?.pluginIssues[alert.key] else { return (alert.title, alert.text) }
            return localizedPlugin(alert, issue: PluginIssue(value: value))
        case "sensor":
            // "<node>|hot" or "<node>|throttled": named from the issue kept, the current one or, once resolved, the last.
            guard let value = snapshot.sensorIssues[subject] ?? previous?.sensorIssues[subject] else { return (alert.title, alert.text) }
            return localizedSensor(alert, issue: SensorIssue(value: value))
        case "unreachable":
            // The cluster is named in the subtitle.
            guard alert.problem else {
                return (String(localized: "Cluster reachable again"), String(localized: "It answers the background checks again."))
            }
            return (String(localized: "Cluster unreachable"), String(localized: "No answer to the last \(unreachableRuns) checks."))
        default:
            return (alert.title, alert.text)
        }
    }

    /// "tool|subject": worded per tool and reason, the namespace/name and severity in the text.
    private static func localizedGitOps(_ alert: Alert, subject key: String, value: String?) -> (title: String, text: String) {
        let subject = GitOpsSubject(key: key)
        let name = subject.title
        guard alert.problem else {
            let title = subject.isFlux
                ? String(localized: "Flux: \(name) is ready again")
                : String(localized: "Argo CD: \(name) is synced and healthy again")
            return (title, subject.label)
        }
        guard let value else { return (alert.title, alert.text) }
        let title = switch gitopsReason(value) {
        case .syncFailed: String(localized: "Argo CD: \(name) sync failed")
        case .degraded: String(localized: "Argo CD: \(name) is degraded")
        case .missing: String(localized: "Argo CD: \(name) has missing resources")
        case .error: String(localized: "Argo CD: \(name) has an error")
        case .outOfSync: String(localized: "Argo CD: \(name) is out of sync")
        case .notReady: String(localized: "Flux: \(name) is not ready")
        case nil: alert.title
        }
        let severity = gitopsSeverity(value) == dataCritical ? ServiceHealth.critical.label : ServiceHealth.warning.label
        return (title, "\(subject.label) · \(severity)")
    }

    /// A plugin finding: "<plugin>: <title>", its detail and severity; once gone, its title.
    private static func localizedPlugin(_ alert: Alert, issue: PluginIssue) -> (title: String, text: String) {
        guard alert.problem else { return (String(localized: "\(issue.title) is healthy again"), issue.pluginName) }
        let severity = issue.severity == dataCritical ? ServiceHealth.critical.label : ServiceHealth.warning.label
        let text = [issue.detail, severity].filter { !$0.isEmpty }.joined(separator: " · ")
        return ("\(issue.pluginName): \(issue.title)", text)
    }

    /// A volume's fill, or a disk failing SMART, on a node named by its hostname.
    /// A node running hot or throttled, worded as on Android.
    private static func localizedSensor(_ alert: Alert, issue: SensorIssue) -> (title: String, text: String) {
        let host = issue.hostname
        if issue.isThrottled {
            guard alert.problem else { return (String(localized: "\(host) no longer throttled"), String(localized: "Node temperatures")) }
            return (String(localized: "\(host) is throttled"), String(localized: "Its cores run well below their speed: check its cooling."))
        }
        guard alert.problem else { return (String(localized: "\(host) back to a normal temperature"), String(localized: "Node temperatures")) }
        return (String(localized: "\(host) runs hot (\(celsiusText(issue.celsius)) °C)"),
                String(localized: "\(issue.label), alert at \(celsiusText(issue.limit)) °C"))
    }

    private static func localizedStorage(_ alert: Alert, issue: StorageIssue, warn: Int) -> (title: String, text: String) {
        let name = issue.name
        let host = issue.hostname
        if issue.isSmart {
            guard alert.problem else { return (String(localized: "\(name) on \(host) passes SMART again"), "") }
            return (String(localized: "SMART failing on \(name) (\(host))"), issue.detail)
        }
        // The "%" goes with the number, so no key carries a literal one.
        guard alert.problem else {
            let threshold = "\(warn) %"
            return (String(localized: "\(name) on \(host) back under \(threshold)"), "")
        }
        let fill = "\(issue.percent) %"
        // A Kubernetes node in DiskPressure is named so whatever its fill; its kubelet may have given no size.
        let title = if issue.detail == kubeDiskPressure {
            String(localized: "\(name) on \(host): DiskPressure")
        } else if issue.severity == dataCritical {
            String(localized: "\(name) on \(host) at \(fill), almost full")
        } else {
            String(localized: "\(name) on \(host) at \(fill)")
        }
        guard issue.sizeBytes > 0 else { return (title, "") }
        let free = formatBytes(issue.freeBytes)
        let size = formatBytes(issue.sizeBytes)
        return (title, String(localized: "\(free) free of \(size)"))
    }

    /// A volume about to be critical or full, by its growth over the last week.
    private static func localizedStorageTrend(_ alert: Alert, issue: StorageTrendIssue) -> (title: String, text: String) {
        let name = issue.name
        let host = issue.hostname
        guard alert.problem else { return (String(localized: "\(name) on \(host) no longer filling up fast"), "") }
        let when = issue.when.localized
        let title = issue.critical
            ? String(localized: "\(name) on \(host) critical \(when)")
            : String(localized: "\(name) on \(host) full \(when)")
        // The "%" goes with the number, so no key carries a literal one.
        let growth = "\(storagePercentText(issue.slopePerDay)) %"
        let fill = "\(storagePercentText(issue.usedPercent)) %"
        return (title, String(localized: "growing ~\(growth) a day, now at \(fill)"))
    }

    static func requestPermission() async -> Bool {
        (try? await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge])) ?? false
    }

    /// The share link a tapped alert opens, on the cluster checked (its `clusterID`); nil without
    /// one, or for an alert about no screen (the Talos certificate alert opens the renewal).
    private static func alertLink(_ alert: Alert, snapshot: ClusterSnapshot, cluster: String) -> URL? {
        guard !cluster.isEmpty, var target = ShareTarget.forAlert(key: alert.key, snapshot: snapshot) else { return nil }
        target.cluster = cluster
        return TalosClient.shareLink(for: target)
    }

    /// The alert kinds, one notification category each ("alert.node"…), so iOS can group and
    /// summarize them per kind.
    static let alertKinds = ["node", "etcd", "cert", "data", "gitops", "checkup", "alertmanager", "storage", "cluster", "plugin"]

    /// The category of an alert: its kind's with its `actions` (see alertCategory), "….private"
    /// with details hidden; "private" for a kind this version does not know. Alertmanager alerts
    /// ("am:…") are of the kind "alertmanager", "unreachable" of the kind "cluster" (alertKind).
    static func category(alertKey: String, actions: [AlertAction] = [], hideDetails: Bool) -> String {
        let kind = alertKind(key: alertKey)
        guard alertKinds.contains(kind) else { return hideDetails ? "private" : "" }
        return alertCategory(kind: kind, actions: actions, hideDetails: hideDetails)
    }

    /// With the app lock on, details are hidden on the lock screen (iOS shows "Notification").
    /// `cluster` names the cluster (the subtitle), `id` keeps the same alert of two clusters apart;
    /// `link` (userInfo "link") is the share link the tap opens; `info` what its actions need.
    private static func post(_ alert: Alert, localized: (title: String, text: String), cluster: String, id: String, link: URL?,
                             actions: [AlertAction], info: [String: String], hideDetails: Bool) async {
        let content = UNMutableNotificationContent()
        content.title = localized.title
        content.subtitle = cluster
        content.body = localized.text
        content.sound = alert.problem ? .default : nil
        content.categoryIdentifier = category(alertKey: alert.key, actions: actions, hideDetails: hideDetails)
        var userInfo = info
        if let link { userInfo["link"] = link.absoluteString }
        content.userInfo = userInfo
        let request = UNNotificationRequest(identifier: id, content: content, trigger: nil)
        try? await UNUserNotificationCenter.current().add(request)
    }

    /// The screenshot mode turned on: the delivered alerts (and Wake-on-LAN answers) name nodes
    /// and clusters, so they go; the jobs' and the freeze reminders' stay.
    static func removeDeliveredAlerts() async {
        let center = UNUserNotificationCenter.current()
        let ids = await center.deliveredNotifications()
            .map(\.request)
            .filter { notificationNamesClusters(category: $0.content.categoryIdentifier, identifier: $0.identifier) }
            .map(\.identifier)
        if !ids.isEmpty { center.removeDeliveredNotifications(withIdentifiers: ids) }
    }

    /// Registers a category per alert kind and set of actions (alertActionSets), plain and
    /// ".private": the private ones keep their previews hidden until the device is unlocked. The
    /// title is hidden with the body (no `.hiddenPreviewsShowTitle`): it names the node. The ones
    /// without actions take the resolved alerts and, like "private", the notifications an older
    /// version delivered.
    static func registerCategories() {
        let placeholder = String(localized: "Talos cluster alert")
        func make(_ id: String, actions: [AlertAction], hidden: Bool) -> UNNotificationCategory {
            let buttons = AlertNotificationActions.notificationActions(actions)
            return hidden
                ? UNNotificationCategory(identifier: id, actions: buttons, intentIdentifiers: [],
                                         hiddenPreviewsBodyPlaceholder: placeholder, options: [])
                : UNNotificationCategory(identifier: id, actions: buttons, intentIdentifiers: [], options: [])
        }
        let alerts = alertKinds.flatMap { kind in
            alertActionSets(kind: kind).flatMap { actions in
                [false, true].map { hidden in
                    make(alertCategory(kind: kind, actions: actions, hideDetails: hidden), actions: actions, hidden: hidden)
                }
            }
        }
        let categories = Set(alerts + [make("private", actions: [], hidden: true), FreezeReminders.notificationCategory,
                                       ConfigTryJob.notificationCategory])
        UNUserNotificationCenter.current().setNotificationCategories(categories)
    }
}

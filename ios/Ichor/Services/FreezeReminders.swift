import Foundation
import IchorCore
import UserNotifications

/// A local notification 5 minutes before an Ichor freeze ends, with "+1 h". Scheduled from the
/// freezes the app last read: Argo CD ends the freeze on its own, the reminder only says so. See
/// plans/roadmap/devops/09-argocd-freeze.md.
@MainActor
enum FreezeReminders {
    nonisolated static let category = "argo-freeze"
    nonisolated static let extendAction = "extend"
    private static let prefix = "argo-freeze|"
    private static let lead: TimeInterval = 5 * 60

    /// What was last scheduled per cluster, so the 2 s polling during a sync does not reschedule.
    private static var scheduled: [String: Set<String>] = [:]

    /// Schedules a reminder for each Ichor freeze of status still running on cluster (its
    /// fingerprint), and drops that cluster's reminders of freezes that are gone.
    static func sync(_ status: ArgoStatus, cluster: String, now: Date = Date()) {
        guard !cluster.isEmpty else { return }
        let nowMs = now.epochMillis
        let running = status.runningIchorFreezes.filter { $0.window.endsAt > nowMs }
        let signature = Set(running.map { "\(identifier(cluster, $0))@\($0.window.endsAt)" })
        guard scheduled[cluster] != signature else { return }
        scheduled[cluster] = signature
        let center = UNUserNotificationCenter.current()
        let keep = Set(running.map { identifier(cluster, $0) })
        let clusterPrefix = prefix + cluster + "|"
        center.getPendingNotificationRequests { requests in
            let gone = requests.map(\.identifier).filter { $0.hasPrefix(clusterPrefix) && !keep.contains($0) }
            UNUserNotificationCenter.current().removePendingNotificationRequests(withIdentifiers: gone)
        }
        for pw in running {
            // Already inside the lead time: its pending reminder (if any) stays as it is.
            guard let fireIn = freezeReminderDelay(endsAt: pw.window.endsAt, lead: lead, now: now) else { continue }
            let request = UNNotificationRequest(identifier: identifier(cluster, pw), content: content(cluster, pw),
                                                trigger: UNTimeIntervalNotificationTrigger(timeInterval: fireIn, repeats: false))
            center.add(request)
        }
    }

    /// One per freeze: two freezes of a project ending together each get their reminder.
    private static func identifier(_ cluster: String, _ pw: ProjectWindow) -> String {
        freezeReminderID(prefix: prefix, cluster: cluster, namespace: pw.project.namespace, project: pw.project.name,
                         window: "\(pw.window.id)")
    }

    private static func content(_ cluster: String, _ pw: ProjectWindow) -> UNMutableNotificationContent {
        let content = UNMutableNotificationContent()
        let scope = (pw.window.namespaces + pw.window.applications).joined(separator: ", ")
        let subject = [pw.project.name, scope].filter { !$0.isEmpty }.joined(separator: " · ")
        let end = Date(epochMillis: pw.window.endsAt).formatted(date: .omitted, time: .shortened)
        content.title = String(localized: "Argo CD freeze ending")
        content.body = String(localized: "The freeze on \(subject) ends at \(end): Argo CD then puts back what was changed by hand.")
        content.categoryIdentifier = category
        content.sound = .default
        content.userInfo = FreezeExtendRequest(cluster: cluster, namespace: pw.project.namespace, project: pw.project.name,
                                               window: pw.window.id, end: pw.window.endsAt).userInfo
        return content
    }

    /// The category with "+1 h": it opens the app, which extends the freeze on its own cluster.
    nonisolated static var notificationCategory: UNNotificationCategory {
        let extend = UNNotificationAction(identifier: extendAction, title: String(localized: "+1 h"), options: [.foreground])
        return UNNotificationCategory(identifier: category, actions: [extend], intentIdentifiers: [],
                                      hiddenPreviewsBodyPlaceholder: String(localized: "Argo CD freeze ending"), options: [])
    }
}

/// "+1 h" on a freeze reminder: which freeze, on which cluster.
struct FreezeExtendRequest: Equatable, Sendable {
    let cluster: String
    let namespace: String
    let project: String
    let window: String
    /// When it was to end, unix ms.
    let end: Int64

    var userInfo: [String: Any] {
        ["cluster": cluster, "namespace": namespace, "project": project, "window": window, "end": end]
    }

    init(cluster: String, namespace: String, project: String, window: String, end: Int64) {
        self.cluster = cluster
        self.namespace = namespace
        self.project = project
        self.window = window
        self.end = end
    }

    init?(userInfo: [AnyHashable: Any]) {
        guard let cluster = userInfo["cluster"] as? String, let namespace = userInfo["namespace"] as? String,
              let project = userInfo["project"] as? String, let window = userInfo["window"] as? String else { return nil }
        self.init(cluster: cluster, namespace: namespace, project: project, window: window,
                  end: (userInfo["end"] as? NSNumber)?.int64Value ?? 0)
    }
}

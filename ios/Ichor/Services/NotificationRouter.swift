import Foundation
import IchorCore
import Observation
import UserNotifications

/// Where a tapped notification or Home Screen quick action should lead; MainNavigation
/// consumes it once the app is open (and unlocked).
@Observable
@MainActor
final class NotificationRouter {
    static let shared = NotificationRouter()

    /// The certificate-expiry alert was tapped: open the renewal screen.
    var pendingRenewal = false

    /// A cluster's quick action was chosen: its fingerprint, to show that cluster.
    var pendingCluster: String?

    /// A freeze reminder was tapped: open the Argo CD sync windows.
    var pendingArgoWindows = false

    /// A GitOps app alert was tapped: open the Argo CD or Flux screen.
    var pendingGitOps: GitOpsDestination?

    /// "+1 h" on a freeze reminder: extend that freeze once the app is open.
    var pendingFreezeExtend: FreezeExtendRequest?

    /// A share link (ichor://open?…) was opened: the screen it names, once unlocked.
    var pendingShareLink: URL?

    /// Reboot, Sync, Reconcile or Silence on an alert: asked of its screen once the alert's link
    /// (pendingShareLink, set with it) has opened it on its cluster.
    var pendingAlertAction: AlertActionRequest?

    /// The same, once that screen is on its way: the screen takes it and shows its confirmation.
    var alertActionRequest: AlertActionRequest?

    /// A config file opened with Ichor (IncomingConfig): its text, for the import preview.
    var pendingImportText: String?
}

/// The screen a GitOps alert leads to.
enum GitOpsDestination: Equatable {
    case argoCD, flux

    /// From an alert key ("gitops:argocd|…", "gitops:flux|…"), nil for other alerts.
    init?(alertKey: String) {
        if alertKey.hasPrefix("gitops:argocd|") {
            self = .argoCD
        } else if alertKey.hasPrefix("gitops:flux|") {
            self = .flux
        } else {
            return nil
        }
    }
}

/// Notification taps (the center keeps its delegate weakly, hence the shared instance).
final class NotificationDelegate: NSObject, UNUserNotificationCenterDelegate {
    static let shared = NotificationDelegate()

    /// A freeze reminder shows while the app is open too (it is the likely moment); the alerts
    /// keep today's behaviour.
    func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification) async -> UNNotificationPresentationOptions {
        if notification.request.content.categoryIdentifier == FreezeReminders.category { return [.banner, .list, .sound] }
        // How a Wake chosen on an alert went: the app may be open by then.
        return notification.request.identifier.hasPrefix(AlertNotificationActions.wakePrefix) ? [.banner, .list] : []
    }

    func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse) async {
        let content = response.notification.request.content
        // A config try's Keep (the app opened, unlocked first) and Revert now.
        if content.categoryIdentifier == ConfigTryJob.category {
            let action = response.actionIdentifier
            await MainActor.run {
                if action == ConfigTryJob.keepAction { ConfigTryJob.shared.keep() }
                if action == ConfigTryJob.revertAction { ConfigTryJob.shared.revert() }
            }
            return
        }
        if content.categoryIdentifier == FreezeReminders.category {
            let extend = response.actionIdentifier == FreezeReminders.extendAction ? FreezeExtendRequest(userInfo: content.userInfo) : nil
            await MainActor.run {
                if let extend { NotificationRouter.shared.pendingFreezeExtend = extend }
                NotificationRouter.shared.pendingArgoWindows = true
            }
            return
        }
        let identifier = response.notification.request.identifier
        let action = response.actionIdentifier
        let cluster = content.userInfo[AlertNotificationActions.clusterKey] as? String ?? ""
        let alertKey = content.userInfo[AlertNotificationActions.alertKey] as? String ?? identifier
        // Snooze and Wake: in the background, the app stays closed.
        if let hours = snoozeHours(actionID: action) {
            AlertNotificationActions.snooze(hours: hours, cluster: cluster, alertKey: alertKey, notification: identifier)
            return
        }
        if action == AlertAction.wake.rawValue {
            let host = content.userInfo[AlertNotificationActions.hostKey] as? String ?? ""
            await AlertNotificationActions.wake(cluster: cluster, alertKey: alertKey, hostname: host)
            return
        }
        // The screen the alert is about, on its cluster (BackgroundMonitor.post); Reboot, Sync,
        // Reconcile and Silence then show that screen's confirmation, nothing runs before it.
        if let link = (content.userInfo["link"] as? String).flatMap(URL.init(string:)) {
            let request = AlertAction(rawValue: action).flatMap { $0.changesCluster ? AlertActionRequest(action: $0, alertKey: alertKey) : nil }
            await MainActor.run {
                NotificationRouter.shared.pendingAlertAction = request
                NotificationRouter.shared.pendingShareLink = link
            }
            return
        }
        // Without a link: the alert key (from an older version, the request identifier);
        // "cert" is the expiry alert. Identifiers now name the cluster too (alertNotificationID).
        if let destination = GitOpsDestination(alertKey: alertKey) {
            await MainActor.run { NotificationRouter.shared.pendingGitOps = destination }
            return
        }
        guard alertKey == "cert" else { return }
        await MainActor.run { NotificationRouter.shared.pendingRenewal = true }
    }
}

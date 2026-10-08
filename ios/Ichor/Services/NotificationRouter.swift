import Foundation
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

    /// A checkup alert was tapped: open the cluster checkup.
    var pendingCheckup = false

    /// "+1 h" on a freeze reminder: extend that freeze once the app is open.
    var pendingFreezeExtend: FreezeExtendRequest?

    /// A share link (ichor://open?…) was opened: the screen it names, once unlocked.
    var pendingShareLink: URL?

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
        notification.request.content.categoryIdentifier == FreezeReminders.category ? [.banner, .list, .sound] : []
    }

    func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse) async {
        let content = response.notification.request.content
        if content.categoryIdentifier == FreezeReminders.category {
            let extend = response.actionIdentifier == FreezeReminders.extendAction ? FreezeExtendRequest(userInfo: content.userInfo) : nil
            await MainActor.run {
                if let extend { NotificationRouter.shared.pendingFreezeExtend = extend }
                NotificationRouter.shared.pendingArgoWindows = true
            }
            return
        }
        // Alert keys are the request identifiers (BackgroundMonitor.post); "cert" is the expiry alert.
        if let destination = GitOpsDestination(alertKey: response.notification.request.identifier) {
            await MainActor.run { NotificationRouter.shared.pendingGitOps = destination }
            return
        }
        // Checkup findings are keyed "checkup:section|kind|subject" (checkupAlerts).
        if response.notification.request.identifier.hasPrefix("checkup:") {
            await MainActor.run { NotificationRouter.shared.pendingCheckup = true }
            return
        }
        guard response.notification.request.identifier == "cert" else { return }
        await MainActor.run { NotificationRouter.shared.pendingRenewal = true }
    }
}

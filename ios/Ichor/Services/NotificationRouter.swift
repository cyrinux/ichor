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

    /// "+1 h" on a freeze reminder: extend that freeze once the app is open.
    var pendingFreezeExtend: FreezeExtendRequest?
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
        guard response.notification.request.identifier == "cert" else { return }
        await MainActor.run { NotificationRouter.shared.pendingRenewal = true }
    }
}

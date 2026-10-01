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
}

/// Notification taps (the center keeps its delegate weakly, hence the shared instance).
final class NotificationDelegate: NSObject, UNUserNotificationCenterDelegate {
    static let shared = NotificationDelegate()

    func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse) async {
        // Alert keys are the request identifiers (BackgroundMonitor.post); "cert" is the expiry alert.
        guard response.notification.request.identifier == "cert" else { return }
        await MainActor.run { NotificationRouter.shared.pendingRenewal = true }
    }
}

import Foundation
import IchorCore
import UserNotifications

extension AlertNotificationActions {
    /// Acknowledge page: finds the incident behind the Alertmanager alert of `alertKey` ("am:<fingerprint>")
    /// on `cluster`, whichever is on screen, and acknowledges it; the alert's repeats stop too. The
    /// outcome replaces the alert's notification. The app stays closed.
    static func acknowledgePage(cluster: String, alertKey: String, notification: String, title: String) async {
        let text: String
        do {
            try await acknowledgeIncident(cluster: cluster, fingerprint: String(alertKey.dropFirst("am:".count)))
            AlertAckStore.acknowledge(cluster: cluster, alertKey: alertKey)
            text = String(localized: "Acknowledged")
        } catch {
            text = String(localized: "Could not acknowledge: \(error.localizedDescription)")
        }
        let content = UNMutableNotificationContent()
        content.title = title
        content.body = text
        // Hidden on the lock screen with the app lock on, like the alert it answers.
        if UserDefaults.standard.bool(forKey: "appLockEnabled") { content.categoryIdentifier = "private" }
        try? await UNUserNotificationCenter.current().add(
            UNNotificationRequest(identifier: notification, content: content, trigger: nil))
    }

    private static func acknowledgeIncident(cluster: String, fingerprint: String) async throws {
        guard let client = await BackgroundMonitor.client(forFingerprint: cluster) else {
            throw OnCallPageError(String(localized: "No open incident for this alert"))
        }
        let config = OnCallStore.read(cluster)
        guard let source = try await AlertmanagerStore.resolve(cluster, with: client) else {
            throw OnCallPageError(String(localized: "No open incident for this alert"))
        }
        let alerts = try await client.alertmanagerAlerts(source)
        guard let incident = try await client.onCallIncidents(config, alerts: alerts).incident(of: fingerprint) else {
            throw OnCallPageError(String(localized: "No open incident for this alert"))
        }
        try await client.onCallAct(config, id: incident.id, verb: .acknowledge)
    }
}

private struct OnCallPageError: LocalizedError {
    let errorDescription: String?

    init(_ message: String) { errorDescription = message }
}

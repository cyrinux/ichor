import Foundation
import IchorCore
import UserNotifications

/// The snoozed alerts (see AlertSnoozes), in the App Group defaults: written by a notification's
/// Snooze action, read by the background check. They last a day at most, so they are not backed up.
enum AlertSnoozeStore {
    private static let key = "monitor.snoozes"

    static func load() -> AlertSnoozes {
        guard let data = SharedStore.defaults.data(forKey: key),
              let snoozes = try? JSONDecoder().decode(AlertSnoozes.self, from: data) else { return AlertSnoozes() }
        return snoozes
    }

    private static func save(_ snoozes: AlertSnoozes) {
        if snoozes.until.isEmpty {
            SharedStore.defaults.removeObject(forKey: key)
        } else if let data = try? JSONEncoder().encode(snoozes) {
            SharedStore.defaults.set(data, forKey: key)
        }
    }

    /// Snoozes `alertKey` of `cluster` (its fingerprint) for `hours`.
    static func snooze(cluster: String, alertKey: String, hours: Int, now: Date = Date()) {
        save(load().pruned(now: now).snoozing(cluster: cluster, key: alertKey, hours: hours, now: now))
    }

    /// The snoozes still running at `now`; the ones over are dropped from the store.
    static func current(now: Date) -> AlertSnoozes {
        let stored = load()
        let pruned = stored.pruned(now: now)
        if pruned != stored { save(pruned) }
        return pruned
    }
}

/// The actions of the alert notifications (CYR-36). Snooze and Wake run in the background;
/// Reboot, Sync, Reconcile and Silence open the app (after Face ID or the passcode) on the
/// alert's screen, which asks for the usual confirmation: nothing changes on the cluster from
/// the lock screen.
enum AlertNotificationActions {
    /// userInfo: the cluster (its fingerprint) and the alert key, for Snooze and Wake; the node's
    /// hostname, for Wake's outcome.
    static let clusterKey = "cluster"
    static let alertKey = "alert"
    static let hostKey = "host"
    /// The identifiers of Wake's outcome notifications: shown while the app is open too.
    static let wakePrefix = "wake|"

    /// The notification actions of `actions`: Snooze as one per duration.
    static func notificationActions(_ actions: [AlertAction]) -> [UNNotificationAction] {
        actions.flatMap { action -> [UNNotificationAction] in
            switch action {
            case .snooze:
                return alertSnoozeHours.map { hours in
                    UNNotificationAction(identifier: snoozeActionID(hours: hours), title: String(localized: "Snooze \(hours) h"), options: [])
                }
            case .wake:
                return [UNNotificationAction(identifier: action.rawValue, title: String(localized: "Wake"), options: [])]
            case .reboot:
                return [UNNotificationAction(identifier: action.rawValue, title: String(localized: "Reboot"),
                                             options: [.foreground, .authenticationRequired, .destructive])]
            case .sync:
                return [UNNotificationAction(identifier: action.rawValue, title: String(localized: "Sync"),
                                             options: [.foreground, .authenticationRequired])]
            case .reconcile:
                return [UNNotificationAction(identifier: action.rawValue, title: String(localized: "Reconcile"),
                                             options: [.foreground, .authenticationRequired])]
            case .silence:
                return [UNNotificationAction(identifier: action.rawValue, title: String(localized: "Silence 1 h"),
                                             options: [.foreground, .authenticationRequired])]
            }
        }
    }

    /// Snooze: the monitor stays quiet on that alert for `hours`; its notification goes away.
    static func snooze(hours: Int, cluster: String, alertKey: String, notification: String) {
        AlertSnoozeStore.snooze(cluster: cluster, alertKey: alertKey, hours: hours)
        UNUserNotificationCenter.current().removeDeliveredNotifications(withIdentifiers: [notification])
    }

    /// Wake: magic packets to the node of `alertKey` ("node:<addr>"), as the node menus send
    /// them, then a notification saying how it went.
    static func wake(cluster: String, alertKey: String, hostname: String) async {
        let parts = alertKey.split(separator: ":", maxSplits: 1).map(String.init)
        guard parts.count == 2, parts[0] == "node", !cluster.isEmpty else { return }
        let address = parts[1]
        let targets = await MainActor.run {
            WakeOnLanStore.shared.loadIfNeeded()
            return WakeOnLanStore.shared.wakeTargets(fingerprint: cluster, node: address)
        }
        guard !targets.isEmpty else { return }
        let name = hostname.isEmpty ? address : hostname
        let text: String
        do {
            var destinations: [String] = []
            for target in targets {
                let destination = try await WakeOnLanSender.send(target)
                if !destinations.contains(destination) { destinations.append(destination) }
            }
            let via = destinations.joined(separator: ", ")
            text = String(localized: "Magic packet sent to \(name) (via \(via))")
        } catch {
            text = String(localized: "Could not send the magic packet: \(error.localizedDescription)")
        }
        let content = UNMutableNotificationContent()
        content.body = text
        // Hidden on the lock screen with the app lock on, like the alert it answers.
        if UserDefaults.standard.bool(forKey: "appLockEnabled") { content.categoryIdentifier = "private" }
        let request = UNNotificationRequest(identifier: wakePrefix + alertKey, content: content, trigger: nil)
        try? await UNUserNotificationCenter.current().add(request)
    }
}

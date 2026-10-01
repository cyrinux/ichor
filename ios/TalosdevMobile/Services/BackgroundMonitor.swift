import BackgroundTasks
import Foundation
import TalosdevMobileCore
import UserNotifications
import WidgetKit

/// Shared with the widget extension (App Group).
enum SharedStore {
    static let suite = "group.name.levis.talosmobile"
    private static let snapshotKey = "snapshot"

    static var defaults: UserDefaults { UserDefaults(suiteName: suite) ?? .standard }

    static func snapshot() -> ClusterSnapshot? {
        defaults.data(forKey: snapshotKey).flatMap { try? JSONDecoder().decode(ClusterSnapshot.self, from: $0) }
    }

    static func save(_ snapshot: ClusterSnapshot?) {
        if let snapshot, let data = try? JSONEncoder().encode(snapshot) {
            defaults.set(data, forKey: snapshotKey)
        } else {
            defaults.removeObject(forKey: snapshotKey)
        }
        WidgetCenter.shared.reloadAllTimelines()
    }
}

/// Best-effort background checks: iOS decides when refresh tasks run (often every 15-60
/// minutes). The config can only be decrypted while the device is unlocked, so checks made
/// while it is locked are skipped. Same alert rules as Android (TalosdevMobileCore.evaluate).
enum BackgroundMonitor {
    static let taskID = "name.levis.talosmobile.refresh"
    static let alertsKey = "monitor.alerts"

    static var alertsEnabled: Bool {
        get { UserDefaults.standard.bool(forKey: alertsKey) }
        set { UserDefaults.standard.set(newValue, forKey: alertsKey) }
    }

    /// Call once, before the app finishes launching.
    static func register() {
        BGTaskScheduler.shared.register(forTaskWithIdentifier: taskID, using: nil) { task in
            guard let task = task as? BGAppRefreshTask else { return }
            schedule()
            let work = Task {
                await check()
                task.setTaskCompleted(success: true)
            }
            task.expirationHandler = { work.cancel() }
        }
    }

    static func schedule() {
        let request = BGAppRefreshTaskRequest(identifier: taskID)
        request.earliestBeginDate = Date(timeIntervalSinceNow: 15 * 60)
        try? BGTaskScheduler.shared.submit(request)
    }

    /// One check: overview + etcd, diffed with the previous snapshot. Silent if the cluster
    /// is unreachable as a whole (e.g. off VPN).
    static func check() async {
        guard let data = SecureConfigStore.load(), let yaml = String(data: data, encoding: .utf8),
              let summary = try? await TalosClient.parse(yaml) else { return }
        let contextName = UserDefaults.standard.string(forKey: "activeContext").flatMap { summary.context(named: $0)?.name } ?? summary.current
        let client = TalosClient(config: yaml, context: contextName)
        guard let overview = try? await client.overview() else { return }
        let etcd = try? await client.etcd()
        let now = Date()
        let current = snapshotOf(overview, etcd: etcd, certNotAfter: summary.context(named: contextName)?.certNotAfter ?? 0, takenAt: now)
        let result = evaluate(previous: SharedStore.snapshot(), current: current, now: now)
        SharedStore.save(result.next)
        guard alertsEnabled else { return }
        let hide = UserDefaults.standard.bool(forKey: "appLockEnabled")
        for alert in result.alerts { await post(alert, hideDetails: hide) }
    }

    static func requestPermission() async -> Bool {
        (try? await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge])) ?? false
    }

    /// With the app lock on, details are hidden on the lock screen (iOS shows "Notification").
    private static func post(_ alert: Alert, hideDetails: Bool) async {
        let content = UNMutableNotificationContent()
        content.title = alert.title
        content.body = alert.text
        content.sound = alert.problem ? .default : nil
        if hideDetails { content.categoryIdentifier = "private" }
        let request = UNNotificationRequest(identifier: alert.key, content: content, trigger: nil)
        try? await UNUserNotificationCenter.current().add(request)
    }

    /// Registers the "private" category: its previews stay hidden until the device is unlocked.
    static func registerCategories() {
        let category = UNNotificationCategory(identifier: "private", actions: [], intentIdentifiers: [],
                                              hiddenPreviewsBodyPlaceholder: "Talos cluster alert",
                                              options: [.hiddenPreviewsShowTitle])
        UNUserNotificationCenter.current().setNotificationCategories([category])
    }
}

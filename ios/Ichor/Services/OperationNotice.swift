import ActivityKit
import Foundation
import IchorCore
import UIKit
import UserNotifications

/// The result of a run followed in the background (an Argo CD sync, a Flux reconcile, a
/// rollout restart, a network test) as a local notification, when Live Activities are off:
/// with them on, the activity already ends with the result. Tapping it opens the run's
/// screen, like the activity (NotificationDelegate reads `kindKey`).
@MainActor
enum OperationNotice {
    /// The userInfo key holding the run's OperationKind.
    nonisolated static let kindKey = "op"

    /// The activity shows the run: no notification needed.
    static var activitiesOn: Bool { ActivityAuthorizationInfo().areActivitiesEnabled }

    /// At the run's start: asks for notifications once, where they will be its only report.
    static func prepare() {
        guard !activitiesOn else { return }
        Task { _ = await BackgroundMonitor.requestPermission() }
    }

    /// Posts the result of `kind`'s run, replacing the previous one of that kind; nothing
    /// while the app is on screen (the run's screen or banner says it) or activities are on.
    static func post(_ kind: OperationKind, title: String, body: String) {
        guard !activitiesOn, UIApplication.shared.applicationState != .active else { return }
        let content = UNMutableNotificationContent()
        content.title = title
        content.body = body
        content.sound = .default
        content.userInfo = [kindKey: kind.rawValue]
        let request = UNNotificationRequest(identifier: "operation-\(kind.rawValue)", content: content, trigger: nil)
        Task { try? await UNUserNotificationCenter.current().add(request) }
    }
}

/// A little time in the background for a followed run, once the app leaves the screen: begun
/// with the run, ended with it or when iOS takes it back, whoever comes first.
@MainActor
final class OperationBackgroundTask {
    private var identifier = UIBackgroundTaskIdentifier.invalid

    func begin(_ name: String) {
        end()
        identifier = UIApplication.shared.beginBackgroundTask(withName: name) {
            // Called on the main thread; the task must end before this returns.
            MainActor.assumeIsolated { self.end() }
        }
    }

    func end() {
        guard identifier != .invalid else { return }
        UIApplication.shared.endBackgroundTask(identifier)
        identifier = .invalid
    }
}

import Foundation
import IchorCore
import UIKit
import UserNotifications

/// The one machine config try the app follows at a time (any node, any screen), so leaving
/// the try screen keeps its countdown and Keep. iOS only lends a little background time: a
/// notification then offers Keep and Revert now, and says the node reverts by itself at the
/// deadline once iOS suspends the app. Talos's own revert is the safety net.
@Observable
@MainActor
final class ConfigTryJob {
    static let shared = ConfigTryJob()

    struct Target: Equatable {
        let node: String
        let hostname: String
    }

    private(set) var target: Target?
    private(set) var progress: ConfigTryProgress?
    private(set) var outcome: ConfigTryOutcome?
    /// Keep or Revert now was asked: no second one until the node answers.
    private(set) var requested = false
    private var handle: ConfigTryHandle?
    private var task: Task<Void, Never>?
    private var backgroundTask = UIBackgroundTaskIdentifier.invalid

    nonisolated static let category = "config-try"
    nonisolated static let keepAction = "config-try.keep"
    nonisolated static let revertAction = "config-try.revert"
    private static let notification = "config-try"

    var isActive: Bool { task != nil }

    /// The node holds the change and waits: it can be kept or reverted.
    var waiting: Bool { isActive && progress?.phase == .trying && !requested }

    /// Keep opens the app first (unlocked, with the app lock); Revert now runs from the
    /// notification, as it only puts the previous config back.
    nonisolated static var notificationCategory: UNNotificationCategory {
        UNNotificationCategory(
            identifier: category,
            actions: [
                UNNotificationAction(identifier: keepAction, title: String(localized: "Keep"),
                                     options: [.foreground, .authenticationRequired]),
                UNNotificationAction(identifier: revertAction, title: String(localized: "Revert now"),
                                     options: [.destructive, .authenticationRequired]),
            ],
            intentIdentifiers: [],
            hiddenPreviewsBodyPlaceholder: String(localized: "A config change is being tried"),
            options: []
        )
    }

    /// Starts the try unless one is followed already; false then.
    @discardableResult
    func start(client: TalosClient, node: String, hostname: String, base: String, draft: String, timeoutSeconds: Int) -> Bool {
        guard !isActive else { return false }
        target = Target(node: node, hostname: hostname)
        progress = nil
        outcome = nil
        requested = false
        // For the notification posted when the app leaves the screen.
        Task { _ = await BackgroundMonitor.requestPermission() }
        let run = client.tryMachineConfig(node: node, base: base, draft: draft, timeoutSeconds: timeoutSeconds)
        handle = run
        task = Task {
            var result = ConfigTryOutcome.failed("")
            for await event in run.events {
                switch event {
                case .progress(let update):
                    progress = update
                    requested = false
                case .done(let done):
                    result = done
                }
            }
            outcome = result
            task = nil
            handle = nil
            endBackgroundTask()
            UNUserNotificationCenter.current().removeDeliveredNotifications(withIdentifiers: [Self.notification])
            announce(Self.announcement(result))
        }
        return true
    }

    func keep() {
        guard waiting else { return }
        requested = true
        handle?.keep()
    }

    func revert() {
        guard waiting else { return }
        requested = true
        handle?.revert()
    }

    /// Forgets a finished try.
    func clear() {
        guard !isActive else { return }
        target = nil
        progress = nil
        outcome = nil
    }

    /// The scene went to the background: borrow some time, and say when the node reverts with
    /// Keep and Revert now at hand.
    func didEnterBackground() {
        guard isActive, let target else { return }
        if backgroundTask == .invalid {
            backgroundTask = UIApplication.shared.beginBackgroundTask(withName: "Config try") {
                // Called on the main thread; the task must end before this returns.
                MainActor.assumeIsolated {
                    self.postSuspended()
                    self.endBackgroundTask()
                }
            }
        }
        post(title: String(localized: "Trying a config change on \(target.hostname)"), body: revertsText)
    }

    /// Back in the foreground: the countdown is on screen again.
    func willEnterForeground() {
        endBackgroundTask()
        UNUserNotificationCenter.current().removeDeliveredNotifications(withIdentifiers: [Self.notification])
    }

    private var revertsText: String {
        guard let deadline = progress?.deadlineDate else {
            return String(localized: "The node reverts by itself unless you keep the change.")
        }
        let time = deadline.formatted(date: .omitted, time: .shortened)
        return String(localized: "Reverts by itself at \(time) unless you keep it.")
    }

    /// iOS is about to suspend the app: Keep can no longer reach the node from here.
    private func postSuspended() {
        guard isActive, let target else { return }
        let time = progress?.deadlineDate?.formatted(date: .omitted, time: .shortened) ?? "—"
        post(title: String(localized: "Trying a config change on \(target.hostname)"),
             body: String(localized: "Ichor was suspended: open it to keep the change. Otherwise the node reverts by itself at \(time)."))
    }

    private func post(title: String, body: String) {
        let content = UNMutableNotificationContent()
        content.title = title
        content.body = body
        content.categoryIdentifier = Self.category
        let request = UNNotificationRequest(identifier: Self.notification, content: content, trigger: nil)
        Task { try? await UNUserNotificationCenter.current().add(request) }
    }

    /// Ends the background task once, whoever comes first (try over, foreground, or expiry).
    private func endBackgroundTask() {
        guard backgroundTask != .invalid else { return }
        UIApplication.shared.endBackgroundTask(backgroundTask)
        backgroundTask = .invalid
    }

    static func announcement(_ outcome: ConfigTryOutcome) -> String {
        switch outcome {
        case .kept: String(localized: "The change is now permanent.")
        case .reverted: String(localized: "The node is back on its previous config.")
        case .failed(let message): message.isEmpty ? String(localized: "The config could not be applied.") : message
        }
    }
}

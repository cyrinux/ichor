import Foundation
import IchorCore
import WatchConnectivity
import WatchKit
import WidgetKit

/// The watch end of WatchLink (CYR-339): receives the phone's WatchPayload, keeps the last one in
/// the watch's own App Group defaults for the complication, and reloads it. Read-only: the watch
/// never reaches a cluster and holds no config.
final class WatchModel: NSObject, ObservableObject, WCSessionDelegate, @unchecked Sendable {
    static let shared = WatchModel()

    @Published private(set) var payload: WatchPayload?

    private let defaults = UserDefaults(suiteName: SharedSnapshot.suite)
    private let lock = NSLock()
    /// Background wake-ups for WatchConnectivity data, completed once nothing is pending.
    private var refreshTasks: [WKWatchConnectivityRefreshBackgroundTask] = []
    private var pendingObservation: NSKeyValueObservation?

    override init() {
        super.init()
        payload = WatchPayload.load(from: defaults)
    }

    func activate() {
        guard WCSession.isSupported() else { return }
        let session = WCSession.default
        if session.delegate == nil {
            session.delegate = self
            pendingObservation = session.observe(\.hasContentPending) { [weak self] _, _ in self?.finishRefreshTasks() }
        }
        if session.activationState != .activated { session.activate() }
    }

    /// watchOS woke the app for the phone's data: kept until it arrived.
    func hold(_ task: WKWatchConnectivityRefreshBackgroundTask) {
        lock.lock()
        refreshTasks.append(task)
        lock.unlock()
        activate()
        finishRefreshTasks()
    }

    private func finishRefreshTasks() {
        let session = WCSession.default
        guard session.activationState == .activated, !session.hasContentPending else { return }
        lock.lock()
        let done = refreshTasks
        refreshTasks = []
        lock.unlock()
        done.forEach { $0.setTaskCompletedWithSnapshot(false) }
    }

    private func receive(_ context: [String: Any]) {
        guard let data = context[WatchPayload.contextKey] as? Data, let received = WatchPayload.decode(data) else {
            return
        }
        if defaults?.data(forKey: WatchPayload.storeKey) != data {
            defaults?.set(data, forKey: WatchPayload.storeKey)
            WidgetCenter.shared.reloadAllTimelines()
        }
        DispatchQueue.main.async { self.payload = received }
    }

    // MARK: - WCSessionDelegate

    func session(
        _ session: WCSession, activationDidCompleteWith activationState: WCSessionActivationState, error: Error?
    ) {
        guard activationState == .activated else { return }
        // The latest context the phone sent while the app was not running.
        receive(session.receivedApplicationContext)
        finishRefreshTasks()
    }

    func session(_ session: WCSession, didReceiveApplicationContext applicationContext: [String: Any]) {
        receive(applicationContext)
        finishRefreshTasks()
    }
}

/// Activates the session at launch and takes watchOS's background wake-ups for the phone's data.
final class WatchAppDelegate: NSObject, WKApplicationDelegate {
    func applicationDidFinishLaunching() {
        WatchModel.shared.activate()
    }

    func handle(_ backgroundTasks: Set<WKRefreshBackgroundTask>) {
        for task in backgroundTasks {
            if let connectivity = task as? WKWatchConnectivityRefreshBackgroundTask {
                WatchModel.shared.hold(connectivity)
            } else {
                task.setTaskCompletedWithSnapshot(false)
            }
        }
    }
}

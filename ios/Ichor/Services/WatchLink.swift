import Foundation
import IchorCore
import WatchConnectivity

/// Sends the paired Apple Watch what the widget shows (CYR-339): the clusters named as on screen
/// and their last checks, as one WatchPayload in the application context (only the latest is
/// kept). Nothing else crosses: no config, token, endpoint or node address. Under the screenshot
/// mode the names are already the masked ones (the widget's list and the monitor's reads are).
final class WatchLink: NSObject, WCSessionDelegate, @unchecked Sendable {
    static let shared = WatchLink()

    private let lock = NSLock()
    /// A send asked before the session was activated, made once it is.
    private var pending = false

    /// Activates the session at launch (also a background launch); harmless without a watch.
    func activate() {
        guard WCSession.isSupported() else { return }
        let session = WCSession.default
        if session.delegate == nil { session.delegate = self }
        if session.activationState != .activated { session.activate() }
    }

    /// Sends the latest payload, built from the App Group the widget reads (SharedStore).
    func send() {
        guard WCSession.isSupported() else { return }
        let session = WCSession.default
        guard session.activationState == .activated else {
            setPending(true)
            activate()
            return
        }
        setPending(false)
        guard session.isPaired, session.isWatchAppInstalled, let data = Self.payload().encoded() else { return }
        do {
            try session.updateApplicationContext([WatchPayload.contextKey: data])
        } catch {
            // The next run or cluster change sends it again; nothing to tell the user.
        }
    }

    private static func payload() -> WatchPayload {
        let defaults = SharedStore.defaults
        let active = SharedSnapshot.activeCluster(from: defaults)
        return watchPayload(clusters: SharedSnapshot.clusters(from: defaults),
                            snapshots: SharedSnapshot.loadAll(from: defaults), active: active,
                            masked: UserDefaults.standard.bool(forKey: PrivacyKeys.enabled), sentAt: Date())
    }

    private func setPending(_ value: Bool) {
        lock.lock()
        pending = value
        lock.unlock()
    }

    private func takePending() -> Bool {
        lock.lock()
        defer { lock.unlock() }
        let was = pending
        pending = false
        return was
    }

    // MARK: - WCSessionDelegate

    func session(_ session: WCSession, activationDidCompleteWith activationState: WCSessionActivationState, error: Error?) {
        guard activationState == .activated else { return }
        // A watch paired or the app installed since the last send gets the current state too.
        if takePending() || session.isWatchAppInstalled { send() }
    }

    func sessionWatchStateDidChange(_ session: WCSession) {
        if session.activationState == .activated { send() }
    }

    func sessionDidBecomeInactive(_ session: WCSession) {}

    /// Switching to another watch: activate again for the new one.
    func sessionDidDeactivate(_ session: WCSession) {
        session.activate()
    }
}

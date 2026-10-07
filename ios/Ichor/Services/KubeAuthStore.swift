import Foundation
import Ichorgo
import IchorCore

/// The sign-ins of the clusters added from a kubeconfig (refresh tokens, keys the user
/// entered): Go's AuthStore, one JSON map (cluster fingerprint → state) sealed like the
/// configs (SecureConfigStore.Item.kubeAuth). Go calls it from its own threads.
final class KubeAuthStore: NSObject, IchorgoAuthStoreProtocol, @unchecked Sendable {
    static let shared = KubeAuthStore()

    private let lock = NSLock()
    /// The map as stored; nil until it could be read (none stored reads as empty).
    private var cache: [String: String]?

    /// Hands the store to Go; again after a restore or a clear, which also drops the tokens Go
    /// holds in memory. Go keeps a weak hold: `shared` keeps it alive.
    static func register() {
        IchorgoSetAuthStore(shared)
    }

    func load(_ key: String?) -> String {
        guard let key, !key.isEmpty else { return "" }
        lock.lock()
        defer { lock.unlock() }
        return readLocked()?[key] ?? ""
    }

    func save(_ key: String?, state: String?) {
        guard let key, !key.isEmpty else { return }
        lock.lock()
        defer { lock.unlock() }
        // Not readable (locked device): writing would drop the other clusters' states.
        guard let current = readLocked() else { return }
        writeLocked(KubeAuthMap.saving(current, key: key, state: state ?? ""))
    }

    /// Every stored state, for a backup; nil when the store cannot be read.
    func all() -> [String: String]? {
        lock.lock()
        defer { lock.unlock() }
        return readLocked()
    }

    /// Adds the states of a restored backup (`restored`: secrets, no session) for the clusters
    /// that have none here: a sign-in this device already holds is the same plus its session.
    /// Then Go forgets the tokens it held.
    func restore(_ restored: [String: String]) {
        lock.lock()
        if let current = readLocked() {
            let merged = current.merging(KubeAuthMap.keeping(restored, fingerprints: Array(restored.keys))) { mine, _ in mine }
            if merged != current { writeLocked(merged) }
        }
        lock.unlock()
        Self.register()
    }

    /// Drops the states of the clusters no longer stored.
    func keep(fingerprints: [String]) {
        lock.lock()
        defer { lock.unlock() }
        guard let current = readLocked() else { return }
        let kept = KubeAuthMap.keeping(current, fingerprints: fingerprints)
        if kept != current { writeLocked(kept) }
    }

    /// Everything was deleted (SecureConfigStore.delete()): forget the copy, and Go its tokens.
    func wipe() {
        lock.lock()
        cache = [:]
        lock.unlock()
        Self.register()
    }

    private func readLocked() -> [String: String]? {
        if let cache { return cache }
        if let data = SecureConfigStore.load(.kubeAuth) {
            cache = KubeAuthMap.decode(data)
        } else if !SecureConfigStore.isStored(.kubeAuth) {
            cache = [:]
        }
        return cache
    }

    private func writeLocked(_ map: [String: String]) {
        if map.isEmpty {
            SecureConfigStore.delete(.kubeAuth)
            cache = [:]
            return
        }
        // Not written (locked device): the copy keeps it until the app is restarted.
        try? SecureConfigStore.save(KubeAuthMap.encode(map), item: .kubeAuth)
        cache = map
    }
}

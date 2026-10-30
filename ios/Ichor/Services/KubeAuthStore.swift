import Foundation
import Ichorgo
import IchorCore

/// The sign-ins of the clusters added from a kubeconfig (refresh tokens, keys the user
/// entered): Go's AuthStore, one JSON map (cluster fingerprint → state) sealed like the
/// configs (SecureConfigStore.Item.kubeAuth). Go calls it from its own threads.
final class KubeAuthStore: NSObject, IchorgoAuthStoreProtocol, Sendable {
    static let shared = KubeAuthStore()

    /// The map as stored; nil until it could be read (none stored reads as empty).
    private let cache = Locked<[String: String]?>(nil)

    /// Hands the store to Go; again after a restore or a clear, which also drops the tokens Go
    /// holds in memory. Go keeps a weak hold: `shared` keeps it alive.
    static func register() {
        IchorgoSetAuthStore(shared)
    }

    func load(_ key: String?) -> String {
        guard let key, !key.isEmpty else { return "" }
        return cache.withLock { Self.read(&$0)?[key] ?? "" }
    }

    func save(_ key: String?, state: String?) {
        guard let key, !key.isEmpty else { return }
        cache.withLock { cache in
            // Not readable (locked device): writing would drop the other clusters' states.
            guard let current = Self.read(&cache) else { return }
            Self.write(KubeAuthMap.saving(current, key: key, state: state ?? ""), to: &cache)
        }
    }

    /// Every stored state, for a backup; nil when the store cannot be read.
    func all() -> [String: String]? {
        cache.withLock { Self.read(&$0) }
    }

    /// Adds the states of a restored backup (`restored`: secrets, no session) for the clusters
    /// that have none here: a sign-in this device already holds is the same plus its session.
    /// Then Go forgets the tokens it held.
    func restore(_ restored: [String: String]) {
        cache.withLock { cache in
            guard let current = Self.read(&cache) else { return }
            let merged = current.merging(KubeAuthMap.keeping(restored, fingerprints: Array(restored.keys))) { mine, _ in
                mine
            }
            if merged != current { Self.write(merged, to: &cache) }
        }
        Self.register()
    }

    /// Drops the states of the clusters no longer stored.
    func keep(fingerprints: [String]) {
        cache.withLock { cache in
            guard let current = Self.read(&cache) else { return }
            let kept = KubeAuthMap.keeping(current, fingerprints: fingerprints)
            if kept != current { Self.write(kept, to: &cache) }
        }
    }

    /// Everything was deleted (SecureConfigStore.delete()): forget the copy, and Go its tokens.
    func wipe() {
        cache.set([:])
        Self.register()
    }

    private static func read(_ cache: inout [String: String]?) -> [String: String]? {
        if let cache { return cache }
        if let data = SecureConfigStore.load(.kubeAuth) {
            cache = KubeAuthMap.decode(data)
        } else if !SecureConfigStore.isStored(.kubeAuth) {
            cache = [:]
        }
        return cache
    }

    private static func write(_ map: [String: String], to cache: inout [String: String]?) {
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

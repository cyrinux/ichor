import CryptoKit
import Foundation
import IchorCore

/// The alert rules (quiet hours, minimum severity, digest; see AlertRules), as one JSON value in
/// the defaults like Android's MonitorStore: set in Settings > Alerts, read by each background
/// check, backed up. A removed cluster's overrides go with it.
enum AlertRulesStore {
    static let key = "monitor.alertRules"

    static func load() -> AlertRules {
        AlertRules.decode(UserDefaults.standard.data(forKey: key))
    }

    static func save(_ rules: AlertRules) {
        if rules == .none {
            UserDefaults.standard.removeObject(forKey: key)
        } else {
            UserDefaults.standard.set(Data(rules.json.utf8), forKey: key)
        }
    }

    /// Only the overrides of `fingerprints`: a removed cluster's go with it.
    static func keep(fingerprints: [String]) {
        let stored = load()
        let kept = stored.keeping(fingerprints)
        if kept != stored { save(kept) }
    }
}

/// The alerts the rules kept quiet (see AlertMutes), per cluster (its monitor key, like the
/// history ring) and alert key, both hashed as Android's AlertMutes: the keys name nodes and
/// apps. Written by the background check, read by "Since you last looked"; not backed up.
enum AlertMuteStore {
    private static let key = "monitor.alertMutes"

    fileprivate static func hash(_ text: String) -> String {
        SHA256.hash(data: Data(text.utf8)).map { String(format: "%02x", $0) }.joined()
    }

    private static func load() -> AlertMutes {
        guard let data = UserDefaults.standard.data(forKey: key),
            let mutes = try? JSONDecoder().decode(AlertMutes.self, from: data)
        else { return AlertMutes() }
        return mutes
    }

    private static func save(_ mutes: AlertMutes) {
        if mutes.entries.isEmpty {
            UserDefaults.standard.removeObject(forKey: key)
        } else if let data = try? JSONEncoder().encode(mutes) {
            UserDefaults.standard.set(data, forKey: key)
        }
    }

    /// Records each of `mutes` (cluster monitor key, alert key, mute) and forgets the ones past
    /// the history's 30 days at `now` (unix ms).
    static func record(_ mutes: [(cluster: String, key: String, mute: AlertMute)], now: Int64) {
        let stored = load()
        let next = mutes.reduce(stored.pruned(now: now)) { all, item in
            guard !item.cluster.isEmpty, !item.key.isEmpty else { return all }
            return all.recording(cluster: hash(item.cluster), key: hash("\(item.cluster)|\(item.key)"), item.mute)
        }
        if next != stored { save(next) }
    }

    /// `cluster`'s mutes as stored now, read once for a screen.
    static func of(cluster: String) -> ClusterMutes { ClusterMutes(cluster: cluster, mutes: load()) }

    /// One cluster's mutes (see of(cluster:)).
    struct ClusterMutes {
        let cluster: String
        let mutes: AlertMutes

        /// The mute of `key` while it was open, from `from` to `to` (nil: still open).
        func during(key: String, from: Int64, to: Int64?) -> AlertMute? {
            guard !cluster.isEmpty, !key.isEmpty else { return nil }
            let store = AlertMuteStore.self
            return mutes.during(
                cluster: store.hash(cluster), key: store.hash("\(cluster)|\(key)"), from: from, to: to)
        }
    }

    /// Only the mutes of `clusters` (monitor keys): a removed cluster's go with it.
    static func keep(clusters: [String]) {
        let stored = load()
        let kept = stored.keeping(clusters: clusters.map(hash))
        if kept != stored { save(kept) }
    }
}

import Foundation

/// What the alert rules did with an alert at `at` (unix ms): `reason` quiet or severity,
/// `until` the quiet window's end (0 for the minimum severity).
public struct AlertMute: Codable, Equatable, Sendable {
    public let reason: String
    public let until: Int64
    public let at: Int64

    public init(reason: String, until: Int64, at: Int64) {
        self.reason = reason
        self.until = until
        self.at = at
    }
}

/// The alerts the rules kept quiet (quiet hours, minimum severity), per cluster and alert key,
/// so the history can say why the phone stayed silent: the latest one per key, kept
/// `keepMillis` (the longest history window), as Android's AlertMutes. Keys name nodes and
/// apps, so the store (AlertMuteStore) passes them hashed: `cluster` and `key` here are what it
/// gives.
public struct AlertMutes: Codable, Equatable, Sendable {
    /// 30 days, the longest history window.
    public static let keepMillis: Int64 = 30 * 86_400_000

    /// Cluster → alert key → its latest mute.
    public private(set) var entries: [String: [String: AlertMute]]

    public init(entries: [String: [String: AlertMute]] = [:]) {
        self.entries = entries
    }

    public func recording(cluster: String, key: String, _ mute: AlertMute) -> AlertMutes {
        guard !cluster.isEmpty, !key.isEmpty else { return self }
        var next = entries
        next[cluster, default: [:]][key] = mute
        return AlertMutes(entries: next)
    }

    /// The latest mute of `key` on `cluster`, or nil.
    public func of(cluster: String, key: String) -> AlertMute? { entries[cluster]?[key] }

    /// The mute of `key` on `cluster` while it was open, from `from` to `to` (nil: still open):
    /// what the history line of that alert says.
    public func during(cluster: String, key: String, from: Int64, to: Int64?) -> AlertMute? {
        guard let mute = of(cluster: cluster, key: key), mute.at >= from, to.map({ mute.at <= $0 }) ?? true else {
            return nil
        }
        return mute
    }

    /// Without the mutes older than `keepMillis` at `now` (unix ms).
    public func pruned(now: Int64) -> AlertMutes {
        let oldest = now - Self.keepMillis
        let kept = entries.mapValues { $0.filter { $0.value.at >= oldest } }.filter { !$0.value.isEmpty }
        return AlertMutes(entries: kept)
    }

    /// Only the mutes of `clusters`: a removed cluster's go with it.
    public func keeping(clusters: [String]) -> AlertMutes {
        let kept = Set(clusters)
        return AlertMutes(entries: entries.filter { kept.contains($0.key) })
    }
}

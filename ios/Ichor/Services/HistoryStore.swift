import CryptoKit
import Foundation
import IchorCore

/// Each cluster's 30-day history ring (CYR-39), by monitor key (monitorClusterKey): one file per
/// cluster, AES-GCM sealed with a device-only Keychain key, excluded from backup, named by a hash
/// so no cluster name shows. Written by the background run, read by the cluster screens. A
/// removed cluster's ring goes with it. When each home was last looked at is kept beside it.
enum HistoryStore {
    private static let sealed = SealedFile(account: "history-key")
    private static let folder = "history"
    private static let lastLookedKey = "history.lastLooked"

    private static func hash(_ text: String) -> String {
        SHA256.hash(data: Data(text.utf8)).map { String(format: "%02x", $0) }.joined()
    }

    private static func location(cluster: String, create: Bool) throws -> URL? {
        let root: URL?
        if create { root = try AppSupport.excludedFolder(folder) } else { root = AppSupport.existingFolder(folder) }
        return root?.appendingPathComponent(hash(cluster))
    }

    /// The ring of `cluster`. A file that reads but does not open (its key gone) starts over; one
    /// that does not read at all (device locked) is left alone.
    static func ring(cluster: String) -> HistoryStoredRing {
        guard !cluster.isEmpty, let file = try? location(cluster: cluster, create: false),
              FileManager.default.fileExists(atPath: file.path) else { return .none }
        if let plain = sealed.read(file) { return .ring(plain) }
        return (try? Data(contentsOf: file)) == nil ? .unreadable : .none
    }

    /// The bytes of `cluster`'s ring, nil without one (or while it cannot be read).
    static func bytes(cluster: String) -> Data? {
        if case .ring(let data) = ring(cluster: cluster) { return data }
        return nil
    }

    static func save(_ ring: Data, cluster: String) throws {
        guard !cluster.isEmpty, let file = try location(cluster: cluster, create: true) else { return }
        try sealed.write(ring, to: file)
    }

    /// Adds `record` to `cluster`'s ring. Best effort: a ring that cannot be read now or that a
    /// newer version wrote, or a record Go refuses, leaves the stored ring as it is.
    static func append(_ record: HistoryRecord, cluster: String, stored: HistoryStoredRing) {
        guard let json = try? record.json() else { return }
        let next = historyNextRing(stored) { ring in
            try TalosClient.historyAppend(ring: ring, record: json, now: record.at)
        }
        if let next { try? save(next, cluster: cluster) }
    }

    /// The alerts open at the end of `ring`: what a run resends for a track it could not read.
    static func openAlerts(in stored: HistoryStoredRing, now: Int64) async -> [HistoryAlertRecord] {
        guard case .ring(let data) = stored,
              let result = try? await TalosClient.historyQuery(ring: data, since: max(0, now - 1), now: now) else { return [] }
        return result.openAlerts
    }

    /// `cluster`'s history over `period` up to now; nil without a ring (or when it cannot be read).
    static func query(cluster: String, period: HistoryPeriod, now: Date = Date()) async -> HistoryQueryResult? {
        guard let data = bytes(cluster: cluster) else { return nil }
        let millis = Int64(now.timeIntervalSince1970 * 1000)
        return try? await TalosClient.historyQuery(ring: data, since: period.since(now: millis), now: millis)
    }

    /// What happened on `cluster` after `lastLooked`; nil without a ring.
    static func since(cluster: String, lastLooked: Int64) async -> HistorySinceSummary? {
        guard let data = bytes(cluster: cluster) else { return nil }
        return try? await TalosClient.historySince(ring: data, lastLooked: lastLooked)
    }

    // MARK: Last looked

    private static func lastLookedMap() -> [String: Int64] {
        (UserDefaults.standard.dictionary(forKey: lastLookedKey) as? [String: NSNumber])?.mapValues(\.int64Value) ?? [:]
    }

    private static func storeLastLooked(_ map: [String: Int64]) {
        if map.isEmpty {
            UserDefaults.standard.removeObject(forKey: lastLookedKey)
        } else {
            UserDefaults.standard.set(map.mapValues { NSNumber(value: $0) }, forKey: lastLookedKey)
        }
    }

    /// When `cluster`'s home was last looked at (epoch ms), nil before the first time.
    static func lastLooked(cluster: String) -> Int64? { lastLookedMap()[cluster] }

    /// Marks `cluster`'s home as looked at `now`; never moves back.
    static func setLastLooked(cluster: String, now: Date = Date()) {
        guard !cluster.isEmpty else { return }
        var map = lastLookedMap()
        map[cluster] = HistoryLastLooked.advanced(stored: map[cluster], to: Int64(now.timeIntervalSince1970 * 1000))
        storeLastLooked(map)
    }

    // MARK: Clean up

    /// Only the clusters still stored (monitor keys) keep their ring and last look.
    static func keep(clusters: [String]) {
        let map = lastLookedMap()
        let kept = HistoryLastLooked.keep(map, clusters: clusters)
        if kept.count != map.count { storeLastLooked(kept) }
        guard let root = AppSupport.existingFolder(folder),
              let files = try? FileManager.default.contentsOfDirectory(at: root, includingPropertiesForKeys: nil) else { return }
        let names = Set(clusters.map(hash))
        for file in files where !names.contains(file.lastPathComponent) {
            try? FileManager.default.removeItem(at: file)
        }
    }

    /// Every ring, every last look, and the key.
    static func wipe() {
        if let root = AppSupport.existingFolder(folder) { try? FileManager.default.removeItem(at: root) }
        storeLastLooked([:])
        sealed.deleteKey()
    }
}

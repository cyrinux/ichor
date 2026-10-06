import Foundation
import CryptoKit
import IchorCore

/// The last data fetched from each cluster (optional, see AppModel.keepLastKnown): one file per
/// cluster and domain, AES-GCM sealed with a device-only Keychain key, excluded from backup.
/// Names are hashes, so node addresses never show in them: a folder per cluster fingerprint
/// (removed with the cluster), a file per fingerprint and domain key.
enum LastKnownStore {
    private static let sealed = SealedFile(account: "last-known-key")

    private static func directory(create: Bool) throws -> URL? {
        if create { return try AppSupport.excludedFolder("offline") }
        return AppSupport.existingFolder("offline")
    }

    private static func hash(_ text: String) -> String {
        SHA256.hash(data: Data(text.utf8)).map { String(format: "%02x", $0) }.joined()
    }

    private static func location(fingerprint: String, key: String, create: Bool) throws -> URL? {
        guard let root = try directory(create: create) else { return nil }
        let folder = root.appendingPathComponent(hash(fingerprint), isDirectory: true)
        if create { try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true) }
        return folder.appendingPathComponent(hash("\(fingerprint)|\(key)"))
    }

    /// The entry of `key`, unless missing or older than a day (then deleted, like one that no
    /// longer opens). Unreadable while the device is locked: nil, the file stays.
    static func read(fingerprint: String, key domainKey: String, now: Date = Date()) -> LastKnownEntry? {
        guard let file = try? location(fingerprint: fingerprint, key: domainKey, create: false),
              FileManager.default.fileExists(atPath: file.path), let plain = sealed.read(file) else { return nil }
        guard let entry = try? JSONDecoder().decode(LastKnownEntry.self, from: plain),
              entry.key == domainKey, entry.isFresh(now: now) else {
            try? FileManager.default.removeItem(at: file)
            return nil
        }
        return entry
    }

    static func save(fingerprint: String, entry: LastKnownEntry) throws {
        guard let file = try location(fingerprint: fingerprint, key: entry.key, create: true) else { return }
        try sealed.write(try JSONEncoder().encode(entry), to: file)
    }

    /// Only the clusters still imported keep theirs.
    static func keep(fingerprints: [String]) {
        guard let root = try? directory(create: false),
              let folders = try? FileManager.default.contentsOfDirectory(at: root, includingPropertiesForKeys: nil) else { return }
        let kept = Set(fingerprints.map(hash))
        for folder in folders where !kept.contains(folder.lastPathComponent) {
            try? FileManager.default.removeItem(at: folder)
        }
    }

    /// Every file, and the key.
    static func wipe() {
        if let root = try? directory(create: false) { try? FileManager.default.removeItem(at: root) }
        sealed.deleteKey()
    }
}

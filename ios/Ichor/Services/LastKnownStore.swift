import Foundation
import CryptoKit
import Security
import IchorCore

/// The last data fetched from each cluster (optional, see AppModel.keepLastKnown): one file per
/// cluster and domain, AES-GCM sealed with a device-only Keychain key, excluded from backup.
/// Names are hashes, so node addresses never show in them: a folder per cluster fingerprint
/// (removed with the cluster), a file per fingerprint and domain key.
enum LastKnownStore {
    private static let account = "last-known-key"

    private static func directory(create: Bool) throws -> URL? {
        let url = try FileManager.default.url(for: .applicationSupportDirectory, in: .userDomainMask,
                                              appropriateFor: nil, create: create).appendingPathComponent("offline", isDirectory: true)
        guard create else { return FileManager.default.fileExists(atPath: url.path) ? url : nil }
        try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
        var excluded = url
        var values = URLResourceValues()
        values.isExcludedFromBackup = true
        try excluded.setResourceValues(values)
        return url
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

    private static func sealingKey(create: Bool) throws -> SymmetricKey? {
        if let bytes = Keychain.read(account) { return SymmetricKey(data: bytes) }
        guard create else { return nil }
        let key = SymmetricKey(size: .bits256)
        try Keychain.write(key.withUnsafeBytes { Data($0) }, account: account)
        return key
    }

    /// The entry of `key`, unless missing or older than a day (then deleted, like one that no
    /// longer opens). Unreadable while the device is locked: nil, the file stays.
    static func read(fingerprint: String, key domainKey: String, now: Date = Date()) -> LastKnownEntry? {
        guard let file = try? location(fingerprint: fingerprint, key: domainKey, create: false),
              let data = try? Data(contentsOf: file), let key = try? sealingKey(create: false) else { return nil }
        guard let box = try? AES.GCM.SealedBox(combined: data),
              let plain = try? AES.GCM.open(box, using: key),
              let entry = try? JSONDecoder().decode(LastKnownEntry.self, from: plain),
              entry.key == domainKey, entry.isFresh(now: now) else {
            try? FileManager.default.removeItem(at: file)
            return nil
        }
        return entry
    }

    static func save(fingerprint: String, entry: LastKnownEntry) throws {
        guard let file = try location(fingerprint: fingerprint, key: entry.key, create: true), let key = try sealingKey(create: true) else { return }
        let sealed = try AES.GCM.seal(try JSONEncoder().encode(entry), using: key)
        guard let data = sealed.combined else { throw KeychainError(status: errSecInternalError) }
        try data.write(to: file, options: [.atomic, .completeFileProtection])
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
        Keychain.delete(account)
    }
}

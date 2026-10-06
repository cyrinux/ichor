import CryptoKit
import Foundation
import Security

/// The app's own folders under Application Support: kept out of backups, as they hold what
/// locates or describes the user's clusters.
enum AppSupport {
    /// `<Application Support>/name`, created if needed and excluded from backup.
    static func excludedFolder(_ name: String) throws -> URL {
        var url = try FileManager.default.url(for: .applicationSupportDirectory, in: .userDomainMask, appropriateFor: nil, create: true)
            .appendingPathComponent(name, isDirectory: true)
        try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
        var values = URLResourceValues()
        values.isExcludedFromBackup = true
        try url.setResourceValues(values)
        return url
    }

    /// `<Application Support>/name` when it already exists; nothing is created.
    static func existingFolder(_ name: String) -> URL? {
        guard let url = try? FileManager.default.url(for: .applicationSupportDirectory, in: .userDomainMask, appropriateFor: nil, create: false)
            .appendingPathComponent(name, isDirectory: true),
            FileManager.default.fileExists(atPath: url.path) else { return nil }
        return url
    }
}

/// Files AES-GCM sealed with a device-only Keychain key under `account`, written atomically
/// with complete protection: what the app keeps on the phone about a cluster.
struct SealedFile: Sendable {
    let account: String

    /// The key; created on first use when `create`, nil when there is none yet.
    func key(create: Bool) throws -> SymmetricKey? {
        if let bytes = Keychain.read(account) { return SymmetricKey(data: bytes) }
        guard create else { return nil }
        let key = SymmetricKey(size: .bits256)
        try Keychain.write(key.withUnsafeBytes { Data($0) }, account: account)
        return key
    }

    /// The file's content, or throws when it or the key does not open (device locked, key gone).
    func open(_ url: URL) throws -> Data {
        guard let key = try key(create: false) else { throw KeychainError(status: errSecItemNotFound) }
        let box = try AES.GCM.SealedBox(combined: Data(contentsOf: url))
        return try AES.GCM.open(box, using: key)
    }

    /// `open`, nil instead of an error.
    func read(_ url: URL) -> Data? { try? open(url) }

    /// `data` sealed (the key is created when missing) and written to `url`.
    func write(_ data: Data, to url: URL) throws {
        guard let key = try key(create: true) else { return }
        let sealed = try AES.GCM.seal(data, using: key)
        guard let combined = sealed.combined else { throw KeychainError(status: errSecInternalError) }
        try combined.write(to: url, options: [.atomic, .completeFileProtection])
    }

    /// Forgets the key: what was sealed with it no longer opens.
    func deleteKey() { Keychain.delete(account) }
}

import Foundation
import CryptoKit
import Security
import IchorCore

/// The last public IP probe of each cluster (see TalosClient.detectPublicIPs), by fingerprint:
/// one file, AES-GCM sealed with a device-only Keychain key, in a folder excluded from backup
/// (the addresses locate the cluster). Forgotten with the cluster.
enum PublicIPStore {
    private static let account = "public-ips-key"

    /// The folder holds the exclusion: an atomic write replaces the file, and its attributes.
    private static func file(create: Bool) throws -> URL? {
        let folder = try FileManager.default.url(for: .applicationSupportDirectory, in: .userDomainMask,
                                                 appropriateFor: nil, create: create).appendingPathComponent("public-ips", isDirectory: true)
        let url = folder.appendingPathComponent("reports")
        guard create else { return FileManager.default.fileExists(atPath: url.path) ? url : nil }
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        var excluded = folder
        var values = URLResourceValues()
        values.isExcludedFromBackup = true
        try excluded.setResourceValues(values)
        return url
    }

    private static func sealingKey(create: Bool) throws -> SymmetricKey? {
        if let bytes = Keychain.read(account) { return SymmetricKey(data: bytes) }
        guard create else { return nil }
        let key = SymmetricKey(size: .bits256)
        try Keychain.write(key.withUnsafeBytes { Data($0) }, account: account)
        return key
    }

    /// Every cluster's report: empty when there is none, nil when the file does not open
    /// (device locked, key gone), so that a write never replaces what it could not read.
    static func read() -> [String: PublicIPReport]? {
        guard let url = try? file(create: false) else { return [:] }
        guard let data = try? Data(contentsOf: url), let key = try? sealingKey(create: false),
              let box = try? AES.GCM.SealedBox(combined: data),
              let plain = try? AES.GCM.open(box, using: key) else { return nil }
        // Opened but from another format: as good as none, overwritten.
        return (try? JSONDecoder().decode([String: PublicIPReport].self, from: plain)) ?? [:]
    }

    /// Saves every cluster's report; nothing when the file could not be read first.
    static func save(_ reports: [String: PublicIPReport]) throws {
        guard read() != nil else { return }
        try write(reports)
    }

    static func wipe() {
        if let url = try? file(create: false) { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        Keychain.delete(account)
    }

    private static func write(_ reports: [String: PublicIPReport]) throws {
        guard !reports.isEmpty else { return wipe() }
        guard let url = try file(create: true), let key = try sealingKey(create: true) else { return }
        let sealed = try AES.GCM.seal(try JSONEncoder().encode(reports), using: key)
        guard let data = sealed.combined else { throw KeychainError(status: errSecInternalError) }
        try data.write(to: url, options: [.atomic, .completeFileProtection])
    }
}

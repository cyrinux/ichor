import Foundation
import IchorCore

/// The last public IP probe of each cluster (see TalosClient.detectPublicIPs), by fingerprint:
/// one file, AES-GCM sealed with a device-only Keychain key, in a folder excluded from backup
/// (the addresses locate the cluster). Forgotten with the cluster.
enum PublicIPStore {
    private static let sealed = SealedFile(account: "public-ips-key")

    /// The folder holds the exclusion: an atomic write replaces the file, and its attributes.
    private static func file(create: Bool) throws -> URL? {
        if create { return try AppSupport.excludedFolder("public-ips").appendingPathComponent("reports") }
        guard let url = AppSupport.existingFolder("public-ips")?.appendingPathComponent("reports"),
              FileManager.default.fileExists(atPath: url.path) else { return nil }
        return url
    }

    /// Every cluster's report: empty when there is none, nil when the file does not open
    /// (device locked, key gone), so that a write never replaces what it could not read.
    static func read() -> [String: PublicIPReport]? {
        guard let url = try? file(create: false) else { return [:] }
        guard let plain = sealed.read(url) else { return nil }
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
        sealed.deleteKey()
    }

    private static func write(_ reports: [String: PublicIPReport]) throws {
        guard !reports.isEmpty else { return wipe() }
        guard let url = try file(create: true) else { return }
        try sealed.write(try JSONEncoder().encode(reports), to: url)
    }
}

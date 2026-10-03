import Foundation
import CryptoKit
import Security

extension AppModel {
    /// Keeps what is saved while masked apart from the real data: "real", or a hash of the words.
    var privacyStorageKey: String {
        guard privacyMask else { return "real" }
        return SHA256.hash(data: Data(privacyWords.utf8)).prefix(8).map { String(format: "%02x", $0) }.joined()
    }
}

/// Encrypted, bounded local observations. The AES key is device-only, accessible
/// when unlocked. Files are excluded from backup, just like packet captures.
struct InsightsStore: Sendable {
    let scope: String
    private var account: String { "insights-key-\(scope)" }
    private func directory() throws -> URL {
        var url = try FileManager.default.url(for: .applicationSupportDirectory, in: .userDomainMask,
                                              appropriateFor: nil, create: true).appendingPathComponent("insights", isDirectory: true)
        try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
        var values = URLResourceValues()
        values.isExcludedFromBackup = true
        try url.setResourceValues(values)
        return url
    }
    private func url(_ kind: String) throws -> URL { try directory().appendingPathComponent("\(scope)-\(kind)") }
    private func key(create: Bool) throws -> SymmetricKey {
        if let bytes = Keychain.read(account) { return SymmetricKey(data: bytes) }
        guard create else { throw KeychainError(status: errSecItemNotFound) }
        let key = SymmetricKey(size: .bits256)
        try Keychain.write(key.withUnsafeBytes { Data($0) }, account: account)
        return key
    }
    func read(_ kind: String) throws -> String? {
        let file = try url(kind)
        guard FileManager.default.fileExists(atPath: file.path) else { return nil }
        let sealed = try AES.GCM.SealedBox(combined: Data(contentsOf: file))
        return String(decoding: try AES.GCM.open(sealed, using: key(create: false)), as: UTF8.self)
    }
    func save(_ kind: String, json: String) throws {
        let sealed = try AES.GCM.seal(Data(json.utf8), using: key(create: true))
        guard let data = sealed.combined else { throw KeychainError(status: errSecInternalError) }
        try data.write(to: url(kind), options: [.atomic, .completeFileProtection])
    }
    func delete(_ kind: String) throws {
        let file = try url(kind)
        if FileManager.default.fileExists(atPath: file.path) { try FileManager.default.removeItem(at: file) }
    }
}

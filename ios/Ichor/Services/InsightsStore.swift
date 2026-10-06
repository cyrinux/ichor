import Foundation
import CryptoKit

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
    private var sealed: SealedFile { SealedFile(account: "insights-key-\(scope)") }
    private func url(_ kind: String) throws -> URL { try AppSupport.excludedFolder("insights").appendingPathComponent("\(scope)-\(kind)") }
    func read(_ kind: String) throws -> String? {
        let file = try url(kind)
        guard FileManager.default.fileExists(atPath: file.path) else { return nil }
        return String(decoding: try sealed.open(file), as: UTF8.self)
    }
    func save(_ kind: String, json: String) throws {
        try sealed.write(Data(json.utf8), to: url(kind))
    }
    func delete(_ kind: String) throws {
        let file = try url(kind)
        if FileManager.default.fileExists(atPath: file.path) { try FileManager.default.removeItem(at: file) }
    }
}

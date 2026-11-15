import Foundation
import IchorCore

/// Each cluster's pairing with its ichor-relay, by context fingerprint, in the Keychain (this
/// device only, never backed up): the ntfy topic is not a secret, but whoever knows it reads
/// the notifications. UserDefaults only lists which clusters are paired.
enum RelayStore {
    private static let indexKey = "relayClusters"

    private static func account(_ fingerprint: String) -> String { "relay-\(fingerprint)" }

    static func read(_ fingerprint: String) -> RelayPairingRecord? {
        guard !fingerprint.isEmpty, let data = Keychain.read(account(fingerprint)) else { return nil }
        return try? JSONDecoder().decode(RelayPairingRecord.self, from: data)
    }

    static func save(_ record: RelayPairingRecord, for fingerprint: String) throws {
        guard !fingerprint.isEmpty else { return }
        try Keychain.write(try JSONEncoder().encode(record), account: account(fingerprint))
        let index = stored()
        if !index.contains(fingerprint) { UserDefaults.standard.set(index + [fingerprint], forKey: indexKey) }
    }

    static func delete(_ fingerprint: String) {
        Keychain.delete(account(fingerprint))
        UserDefaults.standard.set(stored().filter { $0 != fingerprint }, forKey: indexKey)
    }

    /// Forgets the pairings of the clusters not in `fingerprints` (the relay keeps the device
    /// until unpaired from another phone or its Secret is edited).
    static func keep(fingerprints: [String]) {
        let index = stored()
        let kept = index.filter(fingerprints.contains)
        guard kept.count != index.count else { return }
        index.filter { !fingerprints.contains($0) }.forEach { Keychain.delete(account($0)) }
        UserDefaults.standard.set(kept, forKey: indexKey)
    }

    private static func stored() -> [String] { UserDefaults.standard.stringArray(forKey: indexKey) ?? [] }
}

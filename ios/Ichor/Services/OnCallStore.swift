import Foundation
import IchorCore

/// Each cluster's on-call integration, token included, by context fingerprint, in the Keychain
/// (this device only, never backed up). UserDefaults only lists which clusters have one.
enum OnCallStore {
    private static let indexKey = "oncallClusters"

    private static func account(_ fingerprint: String) -> String { "oncall-\(fingerprint)" }

    static func read(_ fingerprint: String) -> OnCallConfig {
        guard let data = Keychain.read(account(fingerprint)),
            let config = try? JSONDecoder().decode(OnCallConfig.self, from: data)
        else { return OnCallConfig() }
        return config
    }

    static func save(_ config: OnCallConfig, for fingerprint: String) throws {
        guard !fingerprint.isEmpty else { return }
        try Keychain.write(try JSONEncoder().encode(config), account: account(fingerprint))
        let index = stored()
        if !index.contains(fingerprint) { UserDefaults.standard.set(index + [fingerprint], forKey: indexKey) }
    }

    /// Removes the integration of the cluster from this device.
    static func remove(_ fingerprint: String) {
        Keychain.delete(account(fingerprint))
        UserDefaults.standard.set(stored().filter { $0 != fingerprint }, forKey: indexKey)
    }

    /// Forgets the settings of the clusters not in `fingerprints`.
    static func keep(fingerprints: [String]) {
        let index = stored()
        let kept = index.filter(fingerprints.contains)
        guard kept.count != index.count else { return }
        index.filter { !fingerprints.contains($0) }.forEach { Keychain.delete(account($0)) }
        UserDefaults.standard.set(kept, forKey: indexKey)
    }

    private static func stored() -> [String] { UserDefaults.standard.stringArray(forKey: indexKey) ?? [] }
}

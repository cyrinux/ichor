import Foundation
import IchorCore

/// Each cluster's metrics source and panels, by context fingerprint, in the Keychain (the
/// source may hold a token or password; this device only, never backed up). UserDefaults
/// only lists which clusters have one, so removed clusters can be forgotten.
enum MetricsStore {
    private static let indexKey = "metricsClusters"

    private static func account(_ fingerprint: String) -> String { "metrics-\(fingerprint)" }

    static func read(_ fingerprint: String) -> MetricsConfig {
        guard let data = Keychain.read(account(fingerprint)),
              let config = try? JSONDecoder().decode(MetricsConfig.self, from: data) else { return MetricsConfig() }
        return config
    }

    static func save(_ config: MetricsConfig, for fingerprint: String) throws {
        guard !fingerprint.isEmpty else { return }
        try Keychain.write(try JSONEncoder().encode(config), account: account(fingerprint))
        let index = stored()
        if !index.contains(fingerprint) { UserDefaults.standard.set(index + [fingerprint], forKey: indexKey) }
    }

    /// Forgets the setups of the clusters not in `fingerprints`.
    static func keep(fingerprints: [String]) {
        let index = stored()
        let kept = index.filter(fingerprints.contains)
        guard kept.count != index.count else { return }
        index.filter { !fingerprints.contains($0) }.forEach { Keychain.delete(account($0)) }
        UserDefaults.standard.set(kept, forKey: indexKey)
    }

    private static func stored() -> [String] { UserDefaults.standard.stringArray(forKey: indexKey) ?? [] }
}

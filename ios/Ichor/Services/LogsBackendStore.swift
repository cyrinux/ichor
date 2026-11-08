import Foundation
import IchorCore

/// Each cluster's log backend and saved queries, by context fingerprint, in the Keychain (a URL
/// source may hold a token or password, a saved query names the cluster's namespaces and pods;
/// this device only, never backed up). UserDefaults only lists which clusters have one.
enum LogsBackendStore {
    private static let indexKey = "logsBackendClusters"

    private static func account(_ fingerprint: String) -> String { "logs-backend-\(fingerprint)" }

    static func read(_ fingerprint: String) -> LogsBackendConfig {
        guard let data = Keychain.read(account(fingerprint)),
            let config = try? JSONDecoder().decode(LogsBackendConfig.self, from: data)
        else { return LogsBackendConfig() }
        return config
    }

    static func save(_ config: LogsBackendConfig, for fingerprint: String) throws {
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

    /// The source saved for the cluster, else the first one found in it (not saved, so a later
    /// install is found too); nil when none.
    static func resolve(_ fingerprint: String, with client: TalosClient) async -> PromSource? {
        if let source = read(fingerprint).source { return source }
        return (try? await client.logsDiscover())?.first
    }
}

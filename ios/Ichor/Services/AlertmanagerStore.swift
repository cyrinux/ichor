import Foundation
import IchorCore

/// Each cluster's Alertmanager source, by context fingerprint, in the Keychain (a URL source may
/// hold a token or password; this device only, never backed up), apart from the metrics source:
/// an Alertmanager is another Service. UserDefaults only lists which clusters have one.
enum AlertmanagerStore {
    private static let indexKey = "alertmanagerClusters"

    private static func account(_ fingerprint: String) -> String { "alertmanager-\(fingerprint)" }

    static func read(_ fingerprint: String) -> AlertmanagerConfig {
        guard let data = Keychain.read(account(fingerprint)),
              let config = try? JSONDecoder().decode(AlertmanagerConfig.self, from: data) else { return AlertmanagerConfig() }
        return config
    }

    static func save(_ config: AlertmanagerConfig, for fingerprint: String) throws {
        guard !fingerprint.isEmpty else { return }
        try Keychain.write(try JSONEncoder().encode(config), account: account(fingerprint))
        let index = stored()
        if !index.contains(fingerprint) { UserDefaults.standard.set(index + [fingerprint], forKey: indexKey) }
    }

    /// Forgets the sources of the clusters not in `fingerprints`.
    static func keep(fingerprints: [String]) {
        let index = stored()
        let kept = index.filter(fingerprints.contains)
        guard kept.count != index.count else { return }
        index.filter { !fingerprints.contains($0) }.forEach { Keychain.delete(account($0)) }
        UserDefaults.standard.set(kept, forKey: indexKey)
    }

    private static func stored() -> [String] { UserDefaults.standard.stringArray(forKey: indexKey) ?? [] }

    /// The source saved for the cluster, else the first one found in it (saved, so the next
    /// read skips the search); nil when none is found. Throws when the search itself fails.
    static func resolve(_ fingerprint: String, with client: TalosClient) async throws -> PromSource? {
        if let source = read(fingerprint).source { return source }
        guard let found = try await client.alertmanagerDiscover().first else { return nil }
        try? save(AlertmanagerConfig(source: found), for: fingerprint)
        return found
    }
}

/// The Alertmanager answers of the cluster on screen, shared by the home cards and the alerts
/// screen; keyed on the context and the screenshot mode generation (AppModel.alertsKey), so
/// another cluster's alerts never show. Remembers a cluster without an Alertmanager, so the
/// home does not search it again at every refresh (a pull on the alerts screen does).
@Observable
@MainActor
final class AlertsStore {
    static let shared = AlertsStore()

    private var key = ""
    private(set) var alerts: AMAlerts?
    /// Searched and none found, for `key`.
    private var noneFound = false

    /// Whether the cluster of `key` was searched and has no Alertmanager.
    func absent(for key: String) -> Bool { key == self.key && noneFound }

    /// What was loaded for `key`, nil when nothing (or for another cluster).
    func alerts(for key: String) -> AMAlerts? { key == self.key ? alerts : nil }

    /// The alerts for the home card, nil when the cluster has no Alertmanager (asked once per key
    /// unless `search` is set).
    func load(with client: TalosClient, key: String, fingerprint: String, search: Bool = false) async throws -> AMAlerts? {
        if key != self.key { reset(key) }
        if noneFound && !search { return nil }
        guard let source = try await AlertmanagerStore.resolve(fingerprint, with: client) else {
            if key == self.key { noneFound = true }
            return nil
        }
        let loaded = try await client.alertmanagerAlerts(source)
        if key == self.key {
            noneFound = false
            alerts = loaded
        }
        return loaded
    }

    /// Keeps what the alerts screen read with every state shown (the card's counts cover them all).
    func remember(_ loaded: AMAlerts, key: String) {
        if key != self.key { reset(key) }
        noneFound = false
        alerts = loaded
    }

    /// A source was set by hand: searched or not, the cluster has one now.
    func sourceSet(key: String) {
        if key != self.key { reset(key) }
        noneFound = false
    }

    private func reset(_ key: String) {
        self.key = key
        alerts = nil
        noneFound = false
    }
}

extension AppModel {
    /// What Alertmanager data belongs to: the context and the screenshot mode generation.
    var alertsKey: String { "\(activeContext)#\(dataGeneration)" }
}

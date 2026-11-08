import Foundation
import IchorCore

/// The Updates screen's last answer per cluster, kept in memory for the session (the GitOps
/// row's "Updates available: N"), and the clusters whose registries the user agreed to have
/// asked (the one-time notice, kept on the device). Nothing runs in the background. Same as
/// Android's UpdatesRepository.
@MainActor @Observable
final class UpdatesSession {
    static let shared = UpdatesSession()

    private(set) var results: [String: ClusterUpdates] = [:]
    private var consented: Set<String>

    private static let consentKey = "ichor.updates.consented"

    private init() {
        consented = Set(UserDefaults.standard.stringArray(forKey: Self.consentKey) ?? [])
    }

    /// The key of the cluster on screen: its fingerprint, else its context name.
    static func key(_ model: AppModel) -> String {
        let fingerprint = model.activeSummary?.fingerprint ?? ""
        return fingerprint.isEmpty ? model.activeContext : fingerprint
    }

    func store(_ updates: ClusterUpdates, for key: String) {
        results[key] = updates
    }

    /// The outdated images and charts of the cluster, once looked up this session.
    func outdated(for key: String) -> Int? {
        results[key].map { updateSections($0).outdated }
    }

    func hasConsented(_ key: String) -> Bool { consented.contains(key) }

    func consent(_ key: String) {
        consented.insert(key)
        UserDefaults.standard.set(Array(consented).sorted(), forKey: Self.consentKey)
    }
}

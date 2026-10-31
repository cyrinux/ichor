import Foundation
import IchorCore

/// The cloud providers' API tokens, one per provider and project (a label the person gives: the
/// API does not name the project), sealed like the configs and never part of a backup: a write
/// token can delete a whole project (SecureConfigStore.Item.cloudTokens).
enum CloudProviderStore {
    static let hetzner = "hetzner"

    /// The projects with a token, sorted.
    static func projects(provider: String) -> [String] {
        let prefix = provider + ":"
        return read().keys.filter { $0.hasPrefix(prefix) }.map { String($0.dropFirst(prefix.count)) }.sorted()
    }

    static func token(provider: String, project: String) -> String {
        read()["\(provider):\(project)"] ?? ""
    }

    /// An empty token removes the project. Refuses to write over a store it cannot read.
    static func save(provider: String, project: String, token: String) throws {
        if SecureConfigStore.isStored(.cloudTokens), SecureConfigStore.load(.cloudTokens) == nil {
            throw SecurityKeyRequiredError()
        }
        let updated = KubeAuthMap.saving(read(), key: "\(provider):\(project)", state: token)
        if updated.isEmpty {
            SecureConfigStore.delete(.cloudTokens)
        } else {
            try SecureConfigStore.save(KubeAuthMap.encode(updated), item: .cloudTokens)
        }
    }

    private static func read() -> [String: String] {
        KubeAuthMap.decode(SecureConfigStore.load(.cloudTokens))
    }
}

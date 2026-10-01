import Foundation
import Observation
import TalosdevMobileCore

/// The optional AI diagnosis: off until the user turns it on. Preferences are in
/// UserDefaults; the API keys only in the Keychain (this device only), one per provider.
@Observable
@MainActor
final class AISettings {
    /// The providers the Go core can ask; empty until loadProviders.
    private(set) var providers: [AIProvider] = []
    /// Key, model and server URL per provider id, read when first needed.
    private var perProvider: [String: AIProviderSettings] = [:]
    private var savedProvider: String?

    var enabled: Bool {
        didSet { UserDefaults.standard.set(enabled, forKey: AIKeys.enabled) }
    }

    /// Replace node names, addresses and domains with placeholders in what is sent.
    var anonymize: Bool {
        didSet { UserDefaults.standard.set(anonymize, forKey: AIKeys.anonymize) }
    }

    init() {
        let defaults = UserDefaults.standard
        enabled = defaults.bool(forKey: AIKeys.enabled)
        anonymize = aiAnonymize(stored: defaults.object(forKey: AIKeys.anonymize) as? Bool)
        savedProvider = defaults.string(forKey: AIKeys.provider)
    }

    var provider: AIProvider? { selectedAIProvider(providers, saved: savedProvider) }

    /// What is set for the selected provider.
    var current: AIProviderSettings {
        guard let id = provider?.id else { return AIProviderSettings() }
        return perProvider[id] ?? AIProviderSettings()
    }

    /// The list comes from Go (no network); asked once.
    func loadProviders() async {
        guard providers.isEmpty, let listed = try? await TalosClient.aiProviders() else { return }
        for item in listed where perProvider[item.id] == nil {
            perProvider[item.id] = Self.stored(item.id)
        }
        providers = listed
    }

    func select(_ id: String) {
        savedProvider = id
        UserDefaults.standard.set(id, forKey: AIKeys.provider)
    }

    /// An empty key removes it from the Keychain.
    func setAPIKey(_ key: String) {
        guard let id = provider?.id, key != current.apiKey else { return }
        perProvider[id, default: AIProviderSettings()].apiKey = key
        if key.isEmpty {
            Keychain.delete(AIKeys.keychainAccount(id))
        } else {
            try? Keychain.write(Data(key.utf8), account: AIKeys.keychainAccount(id))
        }
    }

    func setModel(_ model: String) {
        guard let id = provider?.id else { return }
        perProvider[id, default: AIProviderSettings()].model = model
        UserDefaults.standard.set(model, forKey: AIKeys.model(id))
    }

    func setBaseURL(_ url: String) {
        guard let id = provider?.id else { return }
        perProvider[id, default: AIProviderSettings()].baseURL = url
        UserDefaults.standard.set(url, forKey: AIKeys.baseURL(id))
    }

    private static func stored(_ id: String) -> AIProviderSettings {
        let defaults = UserDefaults.standard
        let key = Keychain.read(AIKeys.keychainAccount(id)).flatMap { String(data: $0, encoding: .utf8) }
        return AIProviderSettings(
            apiKey: key ?? "",
            model: defaults.string(forKey: AIKeys.model(id)) ?? "",
            baseURL: defaults.string(forKey: AIKeys.baseURL(id)) ?? ""
        )
    }
}

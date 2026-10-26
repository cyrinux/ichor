import Foundation
import Observation
import IchorCore

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

    /// The model on the phone (Go's "device" provider): no key, no server.
    static let deviceProvider = "device"

    /// The selected provider runs on the phone.
    var onPhone: Bool { provider?.id == Self.deviceProvider }

    /// The model the provider uses: the one set, else its default.
    var effectiveModel: String { current.model.isEmpty ? provider?.defaultModel ?? "" : current.model }

    /// A question can be asked from the app now: with a key or a server, or, on the phone,
    /// with the model's file there.
    var canAsk: Bool {
        guard onPhone else { return current.canAsk }
        _ = ModelDownloads.shared.version
        return LocalModelStore.shared.file(for: effectiveModel) != nil
    }

    /// Who answers, as the AI screens name it ("Ask %@", "Waiting for %@…").
    var providerName: String {
        onPhone ? String(localized: "the model on this phone") : provider?.name ?? ""
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

    /// What a backup keeps of the AI settings, and the API keys by provider id (empty unless
    /// `includeKeys`: they go in the backup only when the user opted in).
    func backup(includeKeys: Bool) async -> (settings: BackupAI, keys: [String: String]) {
        await loadProviders()
        var models: [String: String] = [:]
        var baseURLs: [String: String] = [:]
        var keys: [String: String] = [:]
        for item in providers {
            let set = perProvider[item.id] ?? Self.stored(item.id)
            if !set.model.isEmpty { models[item.id] = set.model }
            if !set.baseURL.isEmpty { baseURLs[item.id] = set.baseURL }
            if includeKeys, !set.apiKey.isEmpty { keys[item.id] = set.apiKey }
        }
        let settings = BackupAI(enabled: enabled, provider: provider?.id, models: models.isEmpty ? nil : models,
                                baseUrls: baseURLs.isEmpty ? nil : baseURLs, anonymize: anonymize)
        return (settings, keys)
    }

    /// Applies a backup's AI settings and keys as `restoredAI` decides (unknown providers dropped,
    /// a server URL only with its key from the same backup, anonymize off only with keys); a
    /// provider without a key in the backup keeps the one this device has.
    func restore(_ backup: BackupAI?, keys: [String: String]) async {
        await loadProviders()
        let restored = restoredAI(backup, keys: keys, known: Set(providers.map(\.id)))
        if let on = restored.enabled { enabled = on }
        if let hide = restored.anonymize { anonymize = hide }
        if let id = restored.provider { select(id) }
        let defaults = UserDefaults.standard
        for (id, model) in restored.models {
            perProvider[id, default: AIProviderSettings()].model = model
            defaults.set(model, forKey: AIKeys.model(id))
        }
        for (id, key) in restored.keys {
            // A key the Keychain refuses is left out, and its server URL with it.
            do {
                try Keychain.write(Data(key.utf8), account: AIKeys.keychainAccount(id))
                perProvider[id, default: AIProviderSettings()].apiKey = key
            } catch {
                continue
            }
            if let url = restored.baseURLs[id] {
                perProvider[id, default: AIProviderSettings()].baseURL = url
                defaults.set(url, forKey: AIKeys.baseURL(id))
            }
        }
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

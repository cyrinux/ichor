import Foundation

// The optional AI diagnosis: the Go core collects a report and asks the model (see
// go/ichorgo/diagnose.go); this is what the app keeps about the user's choices.

/// A model provider, as listed by the Go core (AIProviders), so both apps show the same defaults.
public struct AIProvider: Decodable, Equatable, Identifiable, Sendable {
    public let id: String
    public let name: String
    public let defaultModel: String
    /// Where the user creates an API key.
    public let keyUrl: String

    public init(id: String, name: String, defaultModel: String, keyUrl: String) {
        self.id = id
        self.name = name
        self.defaultModel = defaultModel
        self.keyUrl = keyUrl
    }
}

/// A model the API key can use (Go AIModels).
public struct AIModel: Decodable, Equatable, Identifiable, Sendable {
    public let id: String
    public let name: String

    public init(id: String, name: String) {
        self.id = id
        self.name = name
    }

    /// "Claude Opus 5.5 (claude-opus-5-5)", or the id alone when the provider gives no other name.
    public var label: String {
        name.isEmpty || name == id ? id : "\(name) (\(id))"
    }
}

/// Where the settings live: UserDefaults for the preferences, the Keychain for the API keys.
/// The model, the server URL and the key are kept per provider, so switching loses nothing.
public enum AIKeys {
    public static let enabled = "aiDiagnosisEnabled"
    public static let provider = "aiProvider"
    public static let anonymize = "aiAnonymize"

    public static func model(_ provider: String) -> String { "aiModel.\(provider)" }
    public static func baseURL(_ provider: String) -> String { "aiBaseURL.\(provider)" }
    /// Keychain account of the provider's API key.
    public static func keychainAccount(_ provider: String) -> String { "ai-api-key.\(provider)" }
}

/// What the user set for one provider. An empty model means the provider's default, an empty
/// server URL its own API.
public struct AIProviderSettings: Equatable, Sendable {
    public var apiKey: String
    public var model: String
    public var baseURL: String

    public init(apiKey: String = "", model: String = "", baseURL: String = "") {
        self.apiKey = apiKey
        self.model = model
        self.baseURL = baseURL
    }

    /// The report can be sent from the app: with a key, or to a server of the user's own
    /// (a local model needs no key).
    public var canAsk: Bool {
        !apiKey.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            || !baseURL.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }
}

/// The provider to use: the saved one while it still exists, else the first listed.
public func selectedAIProvider(_ providers: [AIProvider], saved: String?) -> AIProvider? {
    providers.first { $0.id == saved } ?? providers.first
}

/// Names and addresses are hidden unless the user turned that off.
public func aiAnonymize(stored: Bool?) -> Bool { stored ?? true }

/// The note sent with the report when the diagnosis is opened from a failed health check.
/// English whatever the app's language: it is read by the model, not by the user.
public func healthFailureNote(_ error: String) -> String {
    "The cluster health check failed: \(error.trimmingCharacters(in: .whitespacesAndNewlines))"
}

/// Size of the report as it is sent (UTF-8), e.g. "12.3 KiB".
public func reportSize(_ report: String) -> String {
    formatBytes(report.utf8.count)
}

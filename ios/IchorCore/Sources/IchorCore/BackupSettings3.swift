import Foundation

// The parts of a backup that format 3 added (CYR-343), in the JSON Android writes too.

/// The AI diagnosis settings of a backup, without the keys. Provider ids this app does not
/// know are dropped on restore.
public struct BackupAI: Codable, Equatable, Sendable {
    public var enabled: Bool?
    /// The selected provider id.
    public var provider: String?
    /// Model by provider id; providers left at their default are left out.
    public var models: [String: String]?
    /// Server URL by provider id; providers on their own API are left out.
    public var baseUrls: [String: String]?
    public var anonymize: Bool?

    public init(
        enabled: Bool? = nil, provider: String? = nil, models: [String: String]? = nil,
        baseUrls: [String: String]? = nil, anonymize: Bool? = nil
    ) {
        self.enabled = enabled
        self.provider = provider
        self.models = models
        self.baseUrls = baseUrls
        self.anonymize = anonymize
    }
}

/// The secrets a backup holds only when the user opted in on the export sheet.
public struct BackupSecrets: Codable, Equatable, Sendable {
    /// API key by AI provider id.
    public var aiKeys: [String: String]?
    /// Token by normalised Git host.
    public var gitHostTokens: [String: String]?

    public init(aiKeys: [String: String]? = nil, gitHostTokens: [String: String]? = nil) {
        self.aiKeys = aiKeys
        self.gitHostTokens = gitHostTokens
    }
}

/// The `secrets` to write: nil (the key left out) without the opt-in or with nothing to hold;
/// empty maps and empty values are left out.
public func backupSecrets(include: Bool, aiKeys: [String: String], gitHostTokens: [String: String]) -> BackupSecrets? {
    guard include else { return nil }
    let keys = aiKeys.filter { !$0.key.isEmpty && !$0.value.isEmpty }
    let tokens = gitHostTokens.filter { !$0.key.isEmpty && !$0.value.isEmpty }
    guard !keys.isEmpty || !tokens.isEmpty else { return nil }
    return BackupSecrets(aiKeys: keys.isEmpty ? nil : keys, gitHostTokens: tokens.isEmpty ? nil : tokens)
}

/// `settings.appLockMode`: whether this device had an app lock, never its secret.
public enum BackupLockMode: String, Sendable {
    case off, biometric, securityKey

    public init(enabled: Bool, securityKey: Bool) {
        self = !enabled ? .off : (securityKey ? .securityKey : .biometric)
    }
}

/// The real-scope pins of the clusters in `fingerprints`, as a backup stores them; nil when none.
public func backupPins(fingerprints: [String], stored: (String) -> PinList) -> [String: [Pin]]? {
    var out: [String: [Pin]] = [:]
    for fp in fingerprints where !fp.isEmpty {
        let list = stored(fp)
        if !list.isEmpty { out[fp] = list.pins }
    }
    return out.isEmpty ? nil : out
}

/// The pins to restore for the clusters restored (`fingerprints`), filtered like stored ones
/// (pinnable screens, at most `PinList.limit`). A fingerprint mapped to an empty list clears its pins.
public func restoredPins(_ payload: BackupPayload, fingerprints: [String]) -> [String: PinList] {
    let known = Set(fingerprints.filter { !$0.isEmpty })
    var out: [String: PinList] = [:]
    for (fp, pins) in payload.pins ?? [:] where known.contains(fp) {
        out[fp] = PinList(pins.filter { Pin.pinnable.contains($0.target.target) })
    }
    return out
}

/// The Git hosts to restore, normalised like typed ones, with the token the backup holds for
/// each (nil: keep the one this device has).
public func restoredGitHosts(_ payload: BackupPayload) -> [(host: GitHost, token: String?)] {
    var tokens: [String: String] = [:]
    for (host, token) in payload.secrets?.gitHostTokens ?? [:] where !token.isEmpty {
        let name = normalizedGitHost(host)
        if !name.isEmpty { tokens[name] = token }
    }
    var seen = Set<String>()
    var out: [(host: GitHost, token: String?)] = []
    for saved in payload.gitHosts ?? [] {
        let name = normalizedGitHost(saved.host)
        guard !name.isEmpty, seen.insert(name).inserted else { continue }
        let aliases = parseGitHostAliases(saved.aliases.joined(separator: " "), host: name)
        out.append((GitHost(host: name, kind: saved.kind, aliases: aliases), tokens[name]))
    }
    return out
}

/// What a restore applies of a backup's AI settings, for the providers the Go core lists.
public struct RestoredAI: Equatable, Sendable {
    public var enabled: Bool?
    public var anonymize: Bool?
    public var provider: String?
    public var models: [String: String] = [:]
    /// Only for providers in `keys`.
    public var baseURLs: [String: String] = [:]
    public var keys: [String: String] = [:]
}

/// The AI settings to restore. A server URL applies only with that provider's key from the
/// same backup, and anonymize off only when the backup brings keys: otherwise a crafted file
/// could send this device's own key, and unmasked cluster data, to another server.
public func restoredAI(_ ai: BackupAI?, keys: [String: String]?, known: Set<String>) -> RestoredAI {
    let keys = (keys ?? [:]).filter {
        known.contains($0.key) && !$0.value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }
    var out = RestoredAI(enabled: ai?.enabled, keys: keys)
    if let anonymize = ai?.anonymize, anonymize || !keys.isEmpty { out.anonymize = anonymize }
    if let id = ai?.provider, known.contains(id) { out.provider = id }
    out.models = (ai?.models ?? [:]).filter { known.contains($0.key) }
    out.baseURLs = (ai?.baseUrls ?? [:]).filter { keys[$0.key] != nil }
    return out
}

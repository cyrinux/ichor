import Foundation

/// The imported plugins as a backup carries them (Go PluginUserExport; the payload's `plugins`,
/// format 5): each one's file and whether it is on. Same JSON as Android's, so they move
/// between platforms; PluginUserRestore checks each file again on the phone it lands on.
public struct PluginBackup: Codable, Equatable, Sendable {
    public var plugins: [PluginBackupItem]

    public init(plugins: [PluginBackupItem] = []) {
        self.plugins = plugins
    }

    private enum CodingKeys: String, CodingKey { case plugins }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        plugins = try c.field(.plugins, [])
    }
}

public struct PluginBackupItem: Codable, Equatable, Sendable {
    public var id: String
    public var enabled: Bool
    public var yaml: String

    public init(id: String, enabled: Bool, yaml: String) {
        self.id = id
        self.enabled = enabled
        self.yaml = yaml
    }

    private enum CodingKeys: String, CodingKey { case id, enabled, yaml }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        enabled = try c.field(.enabled, false)
        yaml = try c.field(.yaml, "")
    }
}

/// Go PluginUserRestore's answer: the ids stored, and the plugins left out with why.
public struct PluginRestoreResult: Decodable, Equatable, Sendable {
    public let restored: [String]
    public let errors: [PluginRestoreError]

    private enum CodingKeys: String, CodingKey { case restored, errors }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        restored = try c.field(.restored, [])
        errors = try c.field(.errors, [])
    }

    /// "widgets: plugin.yaml:3: …; gadgets: …": the plugins left out, for the restore's message.
    public var skippedLine: String {
        errors.map { "\($0.id): \($0.error)" }.joined(separator: "; ")
    }
}

public struct PluginRestoreError: Decodable, Equatable, Sendable {
    public let id: String
    public let error: String

    private enum CodingKeys: String, CodingKey { case id, error }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        error = try c.field(.error, "")
    }
}

/// What a scanned code holds (Go PluginQRPayload): a plugin link to download, or the plugin
/// file itself. Either then goes through the check and the consent sheet.
public enum PluginQRPayload: Decodable, Equatable, Sendable {
    case url(String)
    case yaml(String)

    private enum CodingKeys: String, CodingKey { case kind, url, yaml }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        switch try c.field(.kind, "") {
        case "url": self = .url(try c.field(.url, ""))
        case "yaml": self = .yaml(try c.field(.yaml, ""))
        case let kind:
            throw DecodingError.dataCorruptedError(forKey: .kind, in: c, debugDescription: "unknown kind \(kind)")
        }
    }
}

/// The plugins to back up: nil when none is imported, so the backup stays format 3 or 4.
public func backupPlugins(_ export: PluginBackup) -> PluginBackup? {
    export.plugins.isEmpty ? nil : export
}

/// The plugins of a restored payload: only a format 5+ one carries them; nil when it holds none.
public func restoredPlugins(_ payload: BackupPayload) -> PluginBackup? {
    guard payload.format >= backupFormatPlugins, let plugins = payload.plugins, !plugins.plugins.isEmpty else {
        return nil
    }
    return plugins
}

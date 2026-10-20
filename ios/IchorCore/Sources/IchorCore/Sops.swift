import Foundation

// Mirrors go/ichorgo/sops.go.

/// A key a SOPS file is encrypted for: `type` age, pgp, kms, gcp_kms, azure_kv or hc_vault.
public struct SopsRecipient: Decodable, Hashable, Sendable {
    public let type: String
    public let id: String

    private enum CodingKeys: String, CodingKey { case type, id }

    public init(type: String, id: String) {
        self.type = type
        self.id = id
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        type = try c.field(.type, "")
        id = try c.field(.id, "")
    }

    /// The id cut for a chip: an age key or a fingerprint by its ends, a URL by its last part.
    public var short: String {
        guard id.count > 20 else { return id }
        if type == "age" || type == "pgp" { return String(id.prefix(10)) + "…" + String(id.suffix(6)) }
        return "…" + String(id.suffix(20))
    }
}

/// A file's SOPS metadata, never its values: who it is encrypted for, how many values are
/// encrypted, and the ones meant to be that are still in clear (a half-encrypted file).
public struct SopsInfo: Decodable, Sendable {
    public let encrypted: Bool
    public let format: String
    public let recipients: [SopsRecipient]
    public let lastModified: String
    public let encryptedKeys: Int
    public let clearKeys: [String]

    private enum CodingKeys: String, CodingKey { case encrypted, format, recipients, lastModified, encryptedKeys, clearKeys }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        encrypted = try c.field(.encrypted, false)
        format = try c.field(.format, "")
        recipients = try c.field(.recipients, [])
        lastModified = try c.field(.lastModified, "")
        encryptedKeys = try c.field(.encryptedKeys, 0)
        clearKeys = try c.field(.clearKeys, [])
    }
}

/// The .sops.yaml creation rule that covers a path.
public struct SopsRule: Decodable, Sendable {
    public let matched: Bool
    public let pathRegex: String
    public let recipients: [SopsRecipient]

    private enum CodingKeys: String, CodingKey { case matched, pathRegex, recipients }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        matched = try c.field(.matched, false)
        pathRegex = try c.field(.pathRegex, "")
        recipients = try c.field(.recipients, [])
    }
}

/// An edit that would commit a clear-text secret (`what`) where `rule` encrypts.
public struct SopsWarning: Decodable, Sendable {
    public let warn: Bool
    /// "Secret" for a Kubernetes Secret (the app words it), else the Talos key.
    public let what: String
    public let rule: SopsRule?

    private enum CodingKeys: String, CodingKey { case warn, what, rule }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        warn = try c.field(.warn, false)
        what = try c.field(.what, "")
        rule = try c.decodeIfPresent(SopsRule.self, forKey: .rule)
    }

    public var isSecret: Bool { what == "Secret" }
    /// A Talos secrets bundle (the app words it); else what is a Talos key shown as is.
    public var isTalosBundle: Bool { what == "TalosSecrets" }
}

/// content with every SOPS-encrypted value shown as ENC[…]: the ciphertext tells nothing. One
/// line at most: an ENC[ in ordinary text must not swallow the lines after it.
public func collapseSopsValues(_ content: String) -> String {
    content.replacingOccurrences(of: #"ENC\[[^\]\n]*\]"#, with: "ENC[…]", options: .regularExpression)
}

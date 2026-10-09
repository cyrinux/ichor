import Foundation

// Mirrors go/ichorgo/kube_configdata.go: a Secret or ConfigMap key by key. A Secret's values
// come only for the one key asked for (KubeConfigData's key), never in screenshot mode.

/// The first certificate of a PEM value; `count` how many the value holds (a chain). Times
/// in Unix seconds.
public struct ConfigDataCert: Decodable, Equatable, Sendable {
    public let subject: String
    public let issuer: String
    public let notBefore: Int64
    public let notAfter: Int64
    public let dnsNames: [String]
    public let count: Int

    /// Days left before the certificate counts as expiring soon.
    public static let warnDays: Int64 = 30

    public init(subject: String = "", issuer: String = "", notBefore: Int64 = 0, notAfter: Int64 = 0,
                dnsNames: [String] = [], count: Int = 1) {
        self.subject = subject
        self.issuer = issuer
        self.notBefore = notBefore
        self.notAfter = notAfter
        self.dnsNames = dnsNames
        self.count = count
    }

    private enum CodingKeys: String, CodingKey { case subject, issuer, notBefore, notAfter, dnsNames, count }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        subject = try c.field(.subject, "")
        issuer = try c.field(.issuer, "")
        notBefore = try c.field(.notBefore, 0)
        notAfter = try c.field(.notAfter, 0)
        dnsNames = try c.field(.dnsNames, [])
        count = try c.field(.count, 0)
    }

    /// Whole days until it expires, negative once expired (rounded towards the past).
    public func daysLeft(now: Int64) -> Int64 {
        let seconds = notAfter - now
        return seconds >= 0 ? seconds / 86_400 : -((-seconds + 86_399) / 86_400)
    }

    /// Bad once expired, warn within `warnDays` days.
    public func tone(now: Int64) -> KubeTone {
        if notAfter <= now { return .bad }
        return daysLeft(now: now) < Self.warnDays ? .warn : .good
    }

    /// Whom it is for: its DNS names, else its subject.
    public var names: [String] { dnsNames.isEmpty ? [subject].filter { !$0.isEmpty } : dnsNames }
}

/// One key: its decoded `size` in bytes, what it looks like (`hint`: json, pem, text, binary),
/// and its `value` when `revealed` (base64 when `base64`, the value being binary).
public struct ConfigDataKey: Decodable, Equatable, Identifiable, Sendable {
    public let key: String
    public let size: Int64
    public let hint: String
    public let revealed: Bool
    public let value: String
    public let base64: Bool
    public let cert: ConfigDataCert?

    public var id: String { key }

    public static let hintJSON = "json"
    public static let hintPEM = "pem"
    public static let hintText = "text"
    public static let hintBinary = "binary"

    public init(key: String, size: Int64 = 0, hint: String = ConfigDataKey.hintText, revealed: Bool = false,
                value: String = "", base64: Bool = false, cert: ConfigDataCert? = nil) {
        self.key = key
        self.size = size
        self.hint = hint
        self.revealed = revealed
        self.value = value
        self.base64 = base64
        self.cert = cert
    }

    private enum CodingKeys: String, CodingKey { case key, size, hint, revealed, value, base64, cert }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        key = try c.field(.key, "")
        size = try c.field(.size, 0)
        hint = try c.field(.hint, Self.hintText)
        revealed = try c.field(.revealed, false)
        value = try c.field(.value, "")
        base64 = try c.field(.base64, false)
        cert = try c.decodeIfPresent(ConfigDataCert.self, forKey: .cert)
    }
}

/// A registry of a docker config Secret; `username` "" in screenshot mode. Never its password.
public struct ConfigDataRegistry: Decodable, Equatable, Identifiable, Sendable {
    public let registry: String
    public let username: String

    public var id: String { registry }

    private enum CodingKeys: String, CodingKey { case registry, username }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        registry = try c.field(.registry, "")
        username = try c.field(.username, "")
    }
}

/// A pod of the namespace using the object, and how (`via`: env, envFrom, volume, projected,
/// imagePullSecret: Kubernetes' field names).
public struct ConfigDataUse: Decodable, Equatable, Identifiable, Sendable {
    public let pod: String
    public let via: [String]

    public var id: String { pod }

    private enum CodingKeys: String, CodingKey { case pod, via }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        pod = try c.field(.pod, "")
        via = try c.field(.via, [])
    }
}

/// What KubeConfigData answers.
public struct KubeConfigData: Decodable, Equatable, Sendable {
    public let kind: String
    /// The Secret's type, "" for a ConfigMap.
    public let type: String
    public let keys: [ConfigDataKey]
    public let registries: [ConfigDataRegistry]
    public let usedBy: [ConfigDataUse]
    /// The pods could not be listed (RBAC): `usedBy` says nothing.
    public let usedByUnknown: Bool

    private enum CodingKeys: String, CodingKey { case kind, type, keys, registries, usedBy, usedByUnknown }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.field(.kind, "")
        type = try c.field(.type, "")
        keys = try c.field(.keys, [])
        registries = try c.field(.registries, [])
        usedBy = try c.field(.usedBy, [])
        usedByUnknown = try c.field(.usedByUnknown, false)
    }

    /// The keys, `revealed` (a read of that one key) in place of its row.
    public func keys(with revealed: ConfigDataKey?) -> [ConfigDataKey] {
        guard let revealed else { return keys }
        return keys.map { $0.key == revealed.key ? revealed : $0 }
    }
}

extension KubeAPIResource {
    /// Secrets and ConfigMaps have a data tab.
    public var hasConfigData: Bool { group.isEmpty && (resource == "secrets" || resource == "configmaps") }

    /// The kind KubeConfigData takes.
    public var configDataKind: String { isSecret ? "Secret" : "ConfigMap" }
}

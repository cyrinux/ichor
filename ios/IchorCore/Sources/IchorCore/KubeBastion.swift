import Foundation

/// The SSH bastion of a cluster (the core's KubeBastionInfo): where it is and the fingerprint of
/// its pinned host key; never the key or a password.
public struct KubeBastionInfo: Decodable, Equatable, Sendable {
    public static let authKey = "key"
    public static let authPassword = "password"
    /// A Talos cluster's Talos API and Kubernetes API both go through the bastion.
    public static let scopeAll = "all"
    /// Only a Talos cluster's Kubernetes API goes through the bastion.
    public static let scopeKube = "kube"

    public let configured: Bool
    public let host: String
    public let user: String
    /// "key" or "password", empty when none is set.
    public let auth: String
    public let hostKeyType: String
    public let hostKeyFingerprint: String
    /// Why this cluster cannot have one: "proxy-url", "demo", "omni", empty otherwise.
    public let unavailable: String
    /// A Talos cluster's choice (scopeAll or scopeKube); empty for a kubeconfig cluster.
    public let scope: String

    public init(
        configured: Bool = false, host: String = "", user: String = "", auth: String = "",
        hostKeyType: String = "", hostKeyFingerprint: String = "", unavailable: String = "", scope: String = ""
    ) {
        self.configured = configured
        self.host = host
        self.user = user
        self.auth = auth
        self.hostKeyType = hostKeyType
        self.hostKeyFingerprint = hostKeyFingerprint
        self.unavailable = unavailable
        self.scope = scope
    }

    private enum CodingKeys: String, CodingKey {
        case configured, host, user, auth, hostKeyType, hostKeyFingerprint, unavailable, scope
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        configured = try c.decodeIfPresent(Bool.self, forKey: .configured) ?? false
        host = try c.decodeIfPresent(String.self, forKey: .host) ?? ""
        user = try c.decodeIfPresent(String.self, forKey: .user) ?? ""
        auth = try c.decodeIfPresent(String.self, forKey: .auth) ?? ""
        hostKeyType = try c.decodeIfPresent(String.self, forKey: .hostKeyType) ?? ""
        hostKeyFingerprint = try c.decodeIfPresent(String.self, forKey: .hostKeyFingerprint) ?? ""
        unavailable = try c.decodeIfPresent(String.self, forKey: .unavailable) ?? ""
        scope = try c.decodeIfPresent(String.self, forKey: .scope) ?? ""
    }

    /// A Talos cluster: its Talos API may go through the bastion too (see scope).
    public var talos: Bool { !scope.isEmpty }

    /// Signs in with a private key (the default for a new bastion).
    public var usesKey: Bool { auth != Self.authPassword }

    /// The host as the cluster's row says it: the default SSH port left out.
    public var displayHost: String { host.hasSuffix(":22") ? String(host.dropLast(3)) : host }

    public static func decode(_ json: String) throws -> KubeBastionInfo {
        try TalosJSON.decode(KubeBastionInfo.self, from: json)
    }
}

/// The host key a bastion presents (read without signing in): shown for the user to compare,
/// then sent back as is to pin it.
public struct KubeBastionHostKey: Decodable, Equatable, Sendable {
    public let host: String
    public let hostKeyType: String
    public let hostKeyFingerprint: String
    /// The key in authorized_keys form: what setting the bastion pins.
    public let hostKey: String

    public init(host: String, hostKeyType: String, hostKeyFingerprint: String, hostKey: String) {
        self.host = host
        self.hostKeyType = hostKeyType
        self.hostKeyFingerprint = hostKeyFingerprint
        self.hostKey = hostKey
    }

    public static func decode(_ json: String) throws -> KubeBastionHostKey {
        try TalosJSON.decode(KubeBastionHostKey.self, from: json)
    }
}

/// What failed on the way through a bastion, from the core's kube-bastion-* error codes.
public enum BastionProblem: String, CaseIterable, Sendable {
    case unreachable = "kube-bastion-unreachable"
    case hostKey = "kube-bastion-host-key"
    case auth = "kube-bastion-auth"
    case key = "kube-bastion-key"
    case forward = "kube-bastion-forward"
}

/// The bastion problem a core error `message` names, nil for any other failure (and for
/// kube-bastion-unavailable, a refusal to set one rather than a failure through it).
public func bastionProblem(_ message: String?) -> BastionProblem? {
    guard let message else { return nil }
    return BastionProblem.allCases.first { containsCode(message, $0.rawValue) }
}

/// `code` as a whole word: not followed by more of a code (kube-bastion-key vs -keys), nor
/// preceded by one.
private func containsCode(_ message: String, _ code: String) -> Bool {
    var start = message.startIndex
    while let range = message.range(of: code, range: start..<message.endIndex) {
        let before = range.lowerBound == message.startIndex ? nil : message[message.index(before: range.lowerBound)]
        let after = range.upperBound == message.endIndex ? nil : message[range.upperBound]
        if !isCodeCharacter(before) && !isCodeCharacter(after) { return true }
        start = range.upperBound
    }
    return false
}

private func isCodeCharacter(_ c: Character?) -> Bool {
    guard let c else { return false }
    return c == "-" || ("a"..."z").contains(c)
}

/// What setting a bastion sends the core: only the fields given (an empty one keeps what is
/// stored); host and user trimmed, secrets as typed.
public struct BastionFields: Equatable, Sendable {
    public var host = ""
    public var user = ""
    public var privateKey = ""
    public var passphrase = ""
    public var password = ""
    public var hostKey = ""
    /// A Talos cluster's scope; empty keeps the stored one.
    public var scope = ""

    public init(
        host: String = "", user: String = "", privateKey: String = "", passphrase: String = "",
        password: String = "", hostKey: String = "", scope: String = ""
    ) {
        self.host = host
        self.user = user
        self.privateKey = privateKey
        self.passphrase = passphrase
        self.password = password
        self.hostKey = hostKey
        self.scope = scope
    }

    public var values: [String: String] {
        let all = [
            "host": host.trimmingCharacters(in: .whitespacesAndNewlines),
            "user": user.trimmingCharacters(in: .whitespacesAndNewlines),
            "privateKey": privateKey,
            "passphrase": passphrase,
            "password": password,
            "hostKey": hostKey,
            "scope": scope,
        ]
        return all.filter { !$0.value.isEmpty }
    }

    /// `values` as the JSON object the core reads.
    public func json() throws -> String {
        let data = try JSONSerialization.data(withJSONObject: values, options: [.sortedKeys])
        return String(decoding: data, as: UTF8.self)
    }
}

/// Whether the cluster's menu offers a bastion: a kubeconfig cluster without a proxy-url of its
/// own, or a Talos cluster not on Omni (its Kubernetes goes through Omni's proxy); never the
/// demo (the core refuses these), not in screenshot mode.
public func bastionOffered(_ context: ContextSummary, masked: Bool) -> Bool {
    guard !context.demo && !masked else { return false }
    return context.isKube ? (context.proxyURL ?? "").isEmpty : !context.omni
}

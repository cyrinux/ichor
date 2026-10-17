import Foundation

// The Git hosts the phone can read and change repositories on (github.com, a GitLab or a
// Gitea/Forgejo instance): one personal access token per host, typed once in Settings. The
// list is kept in UserDefaults without the tokens; each token lives only in the Keychain and
// only inside the JSON passed to the Go core (go/ichorgo/gitforge.go). Same records as
// Android (model/GitHost.kt).

/// The forge's API, as the Go core names it; auto tells it from the host name.
public enum GitHostKind: String, CaseIterable, Codable, Hashable, Sendable, WireEnum {
    case auto
    case github
    case gitlab
    case gitea

    public static let wireFallback = GitHostKind.auto
}

/// One Git host, as listed in Settings. No token in it: that is in the Keychain.
public struct GitHost: Codable, Hashable, Identifiable, Sendable {
    /// The host name, normalised ("github.com", "gitlab.example.org:8443").
    public let host: String
    public let kind: GitHostKind

    public var id: String { host }

    public init(host: String, kind: GitHostKind = .auto) {
        self.host = host
        self.kind = kind
    }

    enum CodingKeys: String, CodingKey { case host, kind }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        host = try c.field(.host, "")
        kind = try c.wire(.kind)
    }
}

/// What the Go core's GitForgeCheck answers: who the token signs in as, and the scopes it lacks.
public struct GitHostCheck: Decodable, Equatable, Sendable {
    public let login: String
    public let host: String
    public let scopesOK: Bool
    public let missing: [String]

    public init(login: String, host: String = "", scopesOK: Bool = true, missing: [String] = []) {
        self.login = login
        self.host = host
        self.scopesOK = scopesOK
        self.missing = missing
    }

    enum CodingKeys: String, CodingKey { case login, host, scopesOK, missing }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        login = try c.field(.login, "")
        host = try c.field(.host, "")
        missing = try c.field(.missing, [String]())
        scopesOK = try c.field(.scopesOK, missing.isEmpty)
    }
}

/// Where the Git hosts are kept: the list and the last signed-in logins in UserDefaults, the
/// tokens in the Keychain.
public enum GitHostKeys {
    public static let hosts = "gitHosts"
    public static let logins = "gitHostLogins"

    /// Keychain account of the host's token.
    public static func keychainAccount(_ host: String) -> String { "git-host:\(host)" }
}

/// The host part of what the user typed: "https://GitHub.com/" → "github.com". The scheme, a
/// user part, the path, the query and spaces are dropped; a port is kept. Empty when nothing is left.
public func normalizedGitHost(_ input: String) -> String {
    var text = input.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
    if let scheme = text.range(of: "://") { text = String(text[scheme.upperBound...]) }
    if let end = text.firstIndex(where: { "/?#".contains($0) }) { text = String(text[..<end]) }
    if let at = text.lastIndex(of: "@") { text = String(text[text.index(after: at)...]) }
    while text.hasSuffix(".") || text.hasSuffix(":") { text.removeLast() }
    return text.contains(where: \.isWhitespace) ? "" : text
}

/// The Go core's host argument: {"kind","baseURL","token"}, the base URL always https.
public func gitHostJSON(host: String, kind: GitHostKind, token: String) -> String {
    struct Wire: Encodable {
        let kind: String
        let baseURL: String
        let token: String
    }
    let wire = Wire(kind: kind.rawValue, baseURL: "https://\(normalizedGitHost(host))", token: token)
    let encoder = JSONEncoder()
    encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
    // Encoding three strings cannot fail.
    let data = (try? encoder.encode(wire)) ?? Data()
    return String(decoding: data, as: UTF8.self)
}

/// The host argument in the built-in demo: answered by the Go core without network.
public let gitHostDemoJSON = #"{"kind":"demo"}"#

public extension Array where Element == GitHost {
    /// The stored list; empty when nothing (or something unreadable) is stored.
    static func stored(_ data: Data?) -> [GitHost] {
        guard let data, let hosts = try? JSONDecoder().decode([GitHost].self, from: data) else { return [] }
        return hosts.filter { !$0.host.isEmpty }
    }

    /// The list as stored.
    func encoded() -> Data {
        (try? JSONEncoder().encode(self)) ?? Data()
    }

    /// A copy with `host` added at the end, or in place of the entry with the same host.
    func upserting(_ host: GitHost) -> [GitHost] {
        guard contains(where: { $0.host == host.host }) else { return self + [host] }
        return map { $0.host == host.host ? host : $0 }
    }

    /// A copy without the entry of `host`.
    func removing(_ host: String) -> [GitHost] {
        filter { $0.host != host }
    }
}

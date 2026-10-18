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
    /// The host name, normalised ("github.com", "gitlab.example.org:8443"): the API is called there.
    public let host: String
    public let kind: GitHostKind
    /// Other names Argo CD or Flux reach the same server by (an SSH or in-cluster address),
    /// normalised. A source on one of them uses this host's token, still sent only to `host`.
    public let aliases: [String]

    public var id: String { host }

    public init(host: String, kind: GitHostKind = .auto, aliases: [String] = []) {
        self.host = host
        self.kind = kind
        self.aliases = aliases
    }

    enum CodingKeys: String, CodingKey { case host, kind, aliases }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        host = try c.field(.host, "")
        kind = try c.wire(.kind)
        // A list saved before aliases existed has none.
        aliases = try c.field(.aliases, [String]())
    }

    /// Whether `host` (any form normalizedGitHost reads) names this server: its host or an alias.
    /// Ports are ignored: a source's host comes from its web link, which has none, and this only
    /// picks whose token to use; the Go core still checks the exact host and port of the repository.
    public func matches(_ host: String) -> Bool {
        let name = gitHostName(normalizedGitHost(host))
        guard !name.isEmpty else { return false }
        return ([self.host] + aliases).contains { gitHostName(normalizedGitHost($0)) == name }
    }
}

/// The most aliases a host keeps.
public let gitHostAliasLimit = 16

/// The aliases typed for host: split on commas, spaces and new lines, each normalised; blanks,
/// host itself and repeats dropped, at most gitHostAliasLimit kept.
public func parseGitHostAliases(_ text: String, host: String = "") -> [String] {
    let own = normalizedGitHost(host)
    var seen = Set<String>()
    let names = text
        .split(whereSeparator: { $0 == "," || $0 == ";" || $0.isWhitespace })
        .map { normalizedGitHost(String($0)) }
        .filter { !$0.isEmpty && $0 != own && seen.insert($0).inserted }
    return Array(names.prefix(gitHostAliasLimit))
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
    text = withoutScpPath(text)
    return text.contains(where: \.isWhitespace) ? "" : text
}

/// "host:org" of a pasted scp address (git@host:org/repo) is the host; a numeric port is kept.
private func withoutScpPath(_ authority: String) -> String {
    guard !authority.hasPrefix("["), let colon = authority.lastIndex(of: ":") else { return authority }
    var name = String(authority[..<colon])
    while name.hasSuffix(".") { name.removeLast() }
    let port = authority[authority.index(after: colon)...]
    return port.allSatisfy(\.isNumber) ? "\(name):\(port)" : name
}

/// The host name of a normalised host, without its port.
public func gitHostName(_ host: String) -> String {
    if host.hasPrefix("["), let close = host.firstIndex(of: "]") { return String(host[...close]) }
    guard let colon = host.lastIndex(of: ":") else { return host }
    return String(host[..<colon])
}

/// The Go core's host argument: {"kind","baseURL","token","aliases"}, the base URL always https.
/// The aliases only let the core accept a repository on another name: requests go to baseURL.
/// No "aliases" key without any.
public func gitHostJSON(host: String, kind: GitHostKind, token: String, aliases: [String] = []) -> String {
    struct Wire: Encodable {
        let kind: String
        let baseURL: String
        let token: String
        let aliases: [String]?
    }
    let names = aliases.map(normalizedGitHost).filter { !$0.isEmpty }
    let wire = Wire(kind: kind.rawValue, baseURL: "https://\(normalizedGitHost(host))", token: token,
                    aliases: names.isEmpty ? nil : names)
    let encoder = JSONEncoder()
    encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
    // Encoding strings cannot fail.
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

    /// The entry `host` names, by its own name or an alias; an entry's own name wins over
    /// another's alias. Nil when none does.
    func resolving(_ host: String) -> GitHost? {
        let name = normalizedGitHost(host)
        guard !name.isEmpty else { return nil }
        return first { normalizedGitHost($0.host) == name } ?? first { $0.matches(name) }
    }

    /// A copy without the entry of `host`.
    func removing(_ host: String) -> [GitHost] {
        filter { $0.host != host }
    }

    /// `url`, a repository, commit or file page built from a source's address, on the web
    /// address of the listed host whose alias names its host: Argo CD or Flux often reach the
    /// server by an in-cluster or SSH name a browser cannot open. A commit link gets GitLab's
    /// "/-/commit/" when the host is GitLab. Unchanged when it is not an https link, already on
    /// a listed host, or no alias names it. Same rule as Android's gitHostWebUrl.
    func webURL(_ url: String) -> String {
        let prefix = "https://"
        guard url.hasPrefix(prefix) else { return url }
        let rest = url.dropFirst(prefix.count)
        let end = rest.firstIndex(of: "/") ?? rest.endIndex
        let name = gitHostName(normalizedGitHost(String(rest[..<end])))
        guard !name.isEmpty, !contains(where: { gitHostName(normalizedGitHost($0.host)) == name }),
              let host = first(where: { $0.matches(name) }) else { return url }
        let base = normalizedGitHost(host.host)
        var path = String(rest[end...])
        if host.kind == .gitlab || host.kind == .auto && base.contains("gitlab") {
            path = path.replacingOccurrences(of: #"(?<!/-)/commit/([0-9a-f]{7,64})$"#, with: "/-/commit/$1",
                                             options: .regularExpression)
        }
        return prefix + base + path
    }
}

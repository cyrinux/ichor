import Foundation
import Observation
import IchorCore

/// The Git hosts of Settings (see IchorCore/GitHosts.swift): the list and the login each token
/// last signed in as in UserDefaults, the tokens only in the Keychain (this device only, like
/// the AI keys). A token is read when a call needs it and never kept in memory or logged.
@Observable
@MainActor
final class GitHostStore {
    private(set) var hosts: [GitHost]
    /// The login of the last successful check, per host; absent until checked.
    private(set) var logins: [String: String]

    init() {
        let defaults = UserDefaults.standard
        hosts = [GitHost].stored(defaults.data(forKey: GitHostKeys.hosts))
        logins = defaults.dictionary(forKey: GitHostKeys.logins) as? [String: String] ?? [:]
    }

    /// A forge page to open: on the listed host when an alias names its host (see
    /// [GitHost].webURL), nil when it is not an http(s) link.
    func browserURL(_ link: String) -> URL? { webURL(hosts.webURL(link)) }

    /// Whether a token is saved for the host `host` names, by its own name or an alias.
    func hasToken(_ host: String) -> Bool { token(baseHost(host)) != nil }

    /// The kind saved for the host `host` names (its own name or an alias), auto when none is listed.
    func kind(of host: String) -> GitHostKind {
        hosts.resolving(host)?.kind ?? .auto
    }

    /// The Go core's host argument for the host `host` names (its own name or an alias): its
    /// base host, kind, aliases and saved token; nil without a token. The token is only ever
    /// sent to the base host.
    func hostJSON(for host: String) -> String? {
        let saved = hosts.resolving(host)
        let base = saved?.host ?? host
        guard let token = token(base) else { return nil }
        return gitHostJSON(host: base, kind: saved?.kind ?? .auto, token: token, aliases: saved?.aliases ?? [])
    }

    /// The listed host `host` names, by its own name or an alias; `host` itself when none does.
    /// The Keychain account of a token is keyed by it.
    private func baseHost(_ host: String) -> String {
        hosts.resolving(host)?.host ?? host
    }

    /// Adds or updates `host`. An empty `token` keeps the saved one; a new token forgets the
    /// login of the old one, unless `login` is what a check of the new token answered.
    func save(_ host: GitHost, token: String, login: String?) throws {
        let token = token.trimmingCharacters(in: .whitespacesAndNewlines)
        if !token.isEmpty, token != self.token(host.host) {
            try Keychain.write(Data(token.utf8), account: GitHostKeys.keychainAccount(host.host))
            setLogin(login, for: host.host)
        } else if let login {
            setLogin(login, for: host.host)
        }
        setHosts(hosts.upserting(host))
    }

    /// Removes `host` from the list and deletes its token from the Keychain.
    func remove(_ host: String) {
        Keychain.delete(GitHostKeys.keychainAccount(host))
        setLogin(nil, for: host)
        setHosts(hosts.removing(host))
    }

    /// Asks the host who `token` signs in as (the saved token when empty); the demo answers
    /// without network. Remembers the login when the checked token is the saved one.
    func check(_ host: GitHost, token: String, demo: Bool) async throws -> GitHostCheck {
        let typed = token.trimmingCharacters(in: .whitespacesAndNewlines)
        let used = typed.isEmpty ? (self.token(host.host) ?? "") : typed
        let json =
            demo ? gitHostDemoJSON : gitHostJSON(host: host.host, kind: host.kind, token: used, aliases: host.aliases)
        let result = try await TalosClient.gitHostCheck(json)
        if !demo, hosts.contains(where: { $0.host == host.host }), used == self.token(host.host) {
            setLogin(result.login, for: host.host)
        }
        return result
    }

    /// The saved token of each listed host, for a backup the user chose to include them in.
    func tokensForBackup() -> [String: String] {
        var out: [String: String] = [:]
        for host in hosts {
            if let token = token(host.host) { out[host.host] = token }
        }
        return out
    }

    /// Adds or replaces the hosts of a backup by host; a host without a token in the backup keeps
    /// the one this device has. A token the Keychain refuses is skipped (typed again later).
    func restore(_ restored: [(host: GitHost, token: String?)]) {
        for entry in restored {
            do {
                try save(entry.host, token: entry.token ?? "", login: nil)
            } catch {
                // Only the token write throws: keep the host, without it.
                try? save(entry.host, token: "", login: nil)
            }
        }
    }

    private func token(_ host: String) -> String? {
        guard let data = Keychain.read(GitHostKeys.keychainAccount(host)),
            let token = String(data: data, encoding: .utf8), !token.isEmpty
        else { return nil }
        return token
    }

    private func setHosts(_ list: [GitHost]) {
        hosts = list
        if list.isEmpty {
            UserDefaults.standard.removeObject(forKey: GitHostKeys.hosts)
        } else {
            UserDefaults.standard.set(list.encoded(), forKey: GitHostKeys.hosts)
        }
    }

    private func setLogin(_ login: String?, for host: String) {
        var copy = logins
        copy[host] = login.flatMap { $0.isEmpty ? nil : $0 }
        logins = copy
        UserDefaults.standard.set(copy, forKey: GitHostKeys.logins)
    }
}

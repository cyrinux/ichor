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

    func hasToken(_ host: String) -> Bool { token(host) != nil }

    /// The kind saved for `host`, auto when it is not listed.
    func kind(of host: String) -> GitHostKind {
        hosts.first { $0.host == host }?.kind ?? .auto
    }

    /// The Go core's host argument for `host` with its saved token; nil without a token.
    func hostJSON(for host: String) -> String? {
        guard let token = token(host) else { return nil }
        return gitHostJSON(host: host, kind: kind(of: host), token: token)
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
        let json = demo ? gitHostDemoJSON : gitHostJSON(host: host.host, kind: host.kind, token: used)
        let result = try await TalosClient.gitHostCheck(json)
        if !demo, hosts.contains(where: { $0.host == host.host }), used == self.token(host.host) {
            setLogin(result.login, for: host.host)
        }
        return result
    }

    private func token(_ host: String) -> String? {
        guard let data = Keychain.read(GitHostKeys.keychainAccount(host)),
              let token = String(data: data, encoding: .utf8), !token.isEmpty else { return nil }
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

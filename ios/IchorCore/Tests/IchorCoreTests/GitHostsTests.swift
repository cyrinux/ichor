import XCTest
@testable import IchorCore

final class GitHostsTests: XCTestCase {
    func testNormalisesTheTypedHost() {
        XCTAssertEqual(normalizedGitHost("https://github.com/"), "github.com")
        XCTAssertEqual(normalizedGitHost("  GitHub.com  "), "github.com")
        XCTAssertEqual(normalizedGitHost("https://gitlab.example.org/group/repo.git"), "gitlab.example.org")
        XCTAssertEqual(normalizedGitHost("http://git.example.org:3000/?tab=repos"), "git.example.org:3000")
        XCTAssertEqual(normalizedGitHost("https://user@git.example.org"), "git.example.org")
        XCTAssertEqual(normalizedGitHost("github.com."), "github.com")
        XCTAssertEqual(normalizedGitHost("   "), "")
        XCTAssertEqual(normalizedGitHost("https://"), "")
        XCTAssertEqual(normalizedGitHost("git example.org"), "")
    }

    func testBuildsTheGoHostArgument() throws {
        let json = gitHostJSON(host: "https://gitlab.example.org/", kind: .gitlab, token: #"glpat-"quoted"\slash/"#)
        let decoded = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(json.utf8)) as? [String: String])
        XCTAssertEqual(decoded, [
            "kind": "gitlab",
            "baseURL": "https://gitlab.example.org",
            "token": #"glpat-"quoted"\slash/"#,
        ])
        XCTAssertEqual(gitHostJSON(host: "github.com", kind: .auto, token: "t"),
                       #"{"baseURL":"https://github.com","kind":"auto","token":"t"}"#)
        XCTAssertEqual(gitHostDemoJSON, #"{"kind":"demo"}"#)
    }

    func testDecodesTheCheck() throws {
        let ok = try TalosJSON.decode(GitHostCheck.self, from: #"{"login":"octocat","host":"github.com","scopesOK":true,"missing":[]}"#)
        XCTAssertEqual(ok, GitHostCheck(login: "octocat", host: "github.com", scopesOK: true, missing: []))

        let short = try TalosJSON.decode(GitHostCheck.self, from: #"{"login":"dev","scopesOK":false,"missing":["write_repository"]}"#)
        XCTAssertEqual(short.missing, ["write_repository"])
        XCTAssertFalse(short.scopesOK)
        XCTAssertEqual(short.host, "")

        // An older core leaving fields out.
        let bare = try TalosJSON.decode(GitHostCheck.self, from: #"{"login":"dev"}"#)
        XCTAssertTrue(bare.scopesOK)
        XCTAssertEqual(bare.missing, [])
    }

    func testKindsUseTheGoWireNames() {
        XCTAssertEqual(GitHostKind.allCases.map(\.rawValue), ["auto", "github", "gitlab", "gitea"])
        XCTAssertEqual(GitHostKind(wire: "forgejo"), .auto)
        XCTAssertEqual(GitHost(host: "github.com").kind, .auto)
    }

    func testStoredListHasNoTokenAndSurvivesARoundTrip() throws {
        let hosts = [GitHost(host: "github.com"), GitHost(host: "git.example.org", kind: .gitea)]
        let data = hosts.encoded()
        XCTAssertEqual([GitHost].stored(data), hosts)
        let text = String(decoding: data, as: UTF8.self)
        XCTAssertFalse(text.contains("token"))

        XCTAssertEqual([GitHost].stored(nil), [])
        XCTAssertEqual([GitHost].stored(Data("not json".utf8)), [])
        // An unknown kind (a newer app) reads as auto.
        XCTAssertEqual([GitHost].stored(Data(#"[{"host":"git.example.org","kind":"bitbucket"}]"#.utf8)),
                       [GitHost(host: "git.example.org")])
    }

    func testUpsertAndRemoveReturnCopies() {
        let hosts = [GitHost(host: "github.com"), GitHost(host: "gitlab.example.org", kind: .gitlab)]
        let added = hosts.upserting(GitHost(host: "git.example.org", kind: .gitea))
        XCTAssertEqual(added.map(\.host), ["github.com", "gitlab.example.org", "git.example.org"])
        XCTAssertEqual(hosts.count, 2)

        let replaced = added.upserting(GitHost(host: "github.com", kind: .github))
        XCTAssertEqual(replaced.map(\.host), ["github.com", "gitlab.example.org", "git.example.org"])
        XCTAssertEqual(replaced.first?.kind, .github)

        XCTAssertEqual(replaced.removing("gitlab.example.org").map(\.host), ["github.com", "git.example.org"])
        XCTAssertEqual(replaced.removing("absent.example.org"), replaced)
    }

    func testParsesTheTypedAliases() {
        XCTAssertEqual(parseGitHostAliases("ssh.git.example.org, gitea-http.gitea.svc.cluster.local:3000"),
                       ["ssh.git.example.org", "gitea-http.gitea.svc.cluster.local:3000"])
        XCTAssertEqual(parseGitHostAliases("SSH.Git.Example.org\n https://git-int.example.org/repo.git ,,  "),
                       ["ssh.git.example.org", "git-int.example.org"])
        // Blanks, the host itself and repeats are dropped.
        XCTAssertEqual(parseGitHostAliases("git.example.org, ssh.git.example.org, SSH.git.example.org, https://", host: "git.example.org"),
                       ["ssh.git.example.org"])
        XCTAssertEqual(parseGitHostAliases("  "), [])
        let many = (1...30).map { "mirror\($0).example.org" }.joined(separator: ",")
        XCTAssertEqual(parseGitHostAliases(many).count, gitHostAliasLimit)
        XCTAssertEqual(parseGitHostAliases(many).first, "mirror1.example.org")
    }

    func testMatchesTheHostOrAnAlias() {
        let host = GitHost(host: "git.example.org", kind: .gitea, aliases: ["ssh.git.example.org", "gitea-http.gitea.svc.cluster.local:3000"])
        XCTAssertTrue(host.matches("git.example.org"))
        XCTAssertTrue(host.matches("https://Git.Example.org/team/repo"))
        XCTAssertTrue(host.matches("ssh.git.example.org"))
        XCTAssertTrue(host.matches("http://gitea-http.gitea.svc.cluster.local:3000/team/repo.git"))
        XCTAssertFalse(host.matches("gitea-http.gitea.svc.cluster.local"))
        XCTAssertFalse(host.matches("other.example.org"))
        XCTAssertFalse(host.matches(""))
    }

    func testResolvesASourceHostByNameOrAlias() {
        let hosts = [
            GitHost(host: "github.com"),
            GitHost(host: "git.example.org", kind: .gitea, aliases: ["ssh.git.example.org", "mirror.example.org"]),
            GitHost(host: "mirror.example.org", kind: .gitlab),
        ]
        XCTAssertEqual(hosts.resolving("ssh.git.example.org")?.host, "git.example.org")
        XCTAssertEqual(hosts.resolving("git.example.org")?.kind, .gitea)
        // An entry's own name wins over another's alias.
        XCTAssertEqual(hosts.resolving("mirror.example.org")?.host, "mirror.example.org")
        XCTAssertNil(hosts.resolving("other.example.org"))
        XCTAssertNil(hosts.resolving(""))
    }

    func testAliasesSurviveARoundTripAndOldListsDecode() {
        let hosts = [GitHost(host: "git.example.org", kind: .gitea, aliases: ["ssh.git.example.org"])]
        XCTAssertEqual([GitHost].stored(hosts.encoded()), hosts)
        // A list saved before aliases existed.
        XCTAssertEqual([GitHost].stored(Data(#"[{"host":"git.example.org","kind":"gitea"}]"#.utf8)),
                       [GitHost(host: "git.example.org", kind: .gitea)])
        XCTAssertEqual([GitHost].stored(Data(#"[{"host":"git.example.org","kind":"gitea"}]"#.utf8)).first?.aliases, [])
    }

    func testHostArgumentCarriesTheAliases() throws {
        let json = gitHostJSON(host: "git.example.org", kind: .gitea, token: "t",
                               aliases: ["ssh.git.example.org", "gitea-http.gitea.svc.cluster.local:3000"])
        XCTAssertEqual(json, #"{"aliases":["ssh.git.example.org","gitea-http.gitea.svc.cluster.local:3000"],"baseURL":"https://git.example.org","kind":"gitea","token":"t"}"#)
        // No key without aliases, as before.
        XCTAssertFalse(gitHostJSON(host: "git.example.org", kind: .gitea, token: "t", aliases: []).contains("aliases"))
    }

    func testTokenAccountIsPerHost() {
        XCTAssertEqual(GitHostKeys.keychainAccount("github.com"), "git-host:github.com")
        XCTAssertNotEqual(GitHostKeys.keychainAccount("github.com"), GitHostKeys.keychainAccount("git.example.org"))
    }
}

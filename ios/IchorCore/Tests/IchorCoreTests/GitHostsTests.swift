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

    func testTokenAccountIsPerHost() {
        XCTAssertEqual(GitHostKeys.keychainAccount("github.com"), "git-host:github.com")
        XCTAssertNotEqual(GitHostKeys.keychainAccount("github.com"), GitHostKeys.keychainAccount("git.example.org"))
    }
}

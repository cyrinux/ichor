import XCTest
@testable import IchorCore

final class GitRepoSearchTests: XCTestCase {
    func testDecodesTheGoAnswer() throws {
        let json =
            #"{"sha":"abc","hits":[{"path":"talos/nodes/cp-1.yaml","line":3,"text":"hostname: cp-1"}],"searched":9,"truncated":true}"#
        let search = try JSONDecoder().decode(GitRepoSearch.self, from: Data(json.utf8))
        XCTAssertEqual(search.hits, [GitSearchHit(path: "talos/nodes/cp-1.yaml", line: 3, text: "hostname: cp-1")])
        XCTAssertEqual(search.searched, 9)
        XCTAssertTrue(search.truncated)
        XCTAssertEqual(search.hits[0].id, "talos/nodes/cp-1.yaml:3")
    }

    func testMissingFieldsDecodeEmpty() throws {
        let search = try JSONDecoder().decode(GitRepoSearch.self, from: Data(#"{"sha":"abc"}"#.utf8))
        XCTAssertEqual(search, GitRepoSearch(sha: "abc"))
    }

    func testHitsByFileInOrder() {
        let search = GitRepoSearch(hits: [
            GitSearchHit(path: "b.yaml", line: 4, text: "x"), GitSearchHit(path: "a.yaml", line: 9, text: "y"),
            GitSearchHit(path: "b.yaml", line: 1, text: "z"), GitSearchHit(path: "a.yaml", line: 2, text: "w"),
        ])
        XCTAssertEqual(search.byFile.map(\.path), ["a.yaml", "b.yaml"])
        XCTAssertEqual(search.byFile[0].hits.map(\.line), [2, 9])
        XCTAssertEqual(search.byFile[1].hits.map(\.line), [1, 4])
    }
}

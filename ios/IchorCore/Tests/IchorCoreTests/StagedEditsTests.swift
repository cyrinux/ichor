import XCTest
@testable import IchorCore

final class StagedEditsTests: XCTestCase {
    private let key = StagedKey(repoURL: "https://git.example.org/example-org/platform.git", baseRef: "main")

    private func file(_ path: String, draft: String = "b: 2\n") -> StagedFile {
        StagedFile(path: path, original: "a: 1\n", fileSha: "sha-\(path)", draft: draft)
    }

    func testStagingAgainReplacesTheFileInPlace() {
        var basket = StagedBasket()
        basket.stage(key, file("apps/a.yaml"))
        basket.stage(key, file("apps/b.yaml"))
        basket.stage(key, file("apps/a.yaml", draft: "c: 3\n"))

        XCTAssertEqual(basket.of(key).map(\.path), ["apps/a.yaml", "apps/b.yaml"])
        XCTAssertEqual(basket.of(key).first?.draft, "c: 3\n")
    }

    func testEachRepositoryAndBranchHasItsOwnFiles() {
        var basket = StagedBasket()
        let other = StagedKey(repoURL: key.repoURL, baseRef: "release")
        basket.stage(key, file("apps/a.yaml"))
        basket.stage(other, file("apps/b.yaml"))
        basket.clear(key)

        XCTAssertTrue(basket.of(key).isEmpty)
        XCTAssertEqual(basket.of(other).count, 1)
        basket.remove(other, path: "apps/b.yaml")
        XCTAssertTrue(basket.files.isEmpty)
    }

    func testAFullPullRequestTakesNoNewFile() {
        var basket = StagedBasket()
        for i in 0..<maxStagedFiles { basket.stage(key, file("apps/f\(i).yaml")) }

        XCTAssertFalse(basket.canStage(key, path: "apps/new.yaml"))
        XCTAssertTrue(basket.canStage(key, path: "apps/f0.yaml"))
    }

    func testTheKeyIsTheBranchTheFilesAreProposedTo() {
        let source = GitSource(
            repoURL: key.repoURL, tracked: "main", revision: "0123456789abcdef0123456789abcdef01234567")
        XCTAssertEqual(StagedKey(source: source), key)
    }

    func testSeveralFilesAreTitledAfterTheirCommonDirectory() {
        XCTAssertEqual(
            stagedDefaultsPath(["apps/podinfo/deployment.yaml", "apps/podinfo/kustomization.yaml"]), "apps/podinfo")
        XCTAssertEqual(stagedDefaultsPath(["apps/podinfo/deployment.yaml", "apps/grafana/values.yaml"]), "apps")
        XCTAssertEqual(stagedDefaultsPath(["talconfig.yaml", "clusters/lab/flux.yaml"]), "talconfig.yaml")
        XCTAssertEqual(stagedDefaultsPath(["apps/a.yaml"]), "apps/a.yaml")
    }

    func testTheOptionsCarryTheFilesOnlyWhenThereAre() throws {
        let one = GitProposal(
            repoURL: key.repoURL, baseRef: "main", path: "a.yaml", content: "a: 1\n", fileSha: "s",
            message: "m", branch: "", title: "t", body: "", draft: false)
        XCTAssertFalse(try one.optionsJSON().contains("\"files\""))

        let several = GitProposal(
            repoURL: key.repoURL, baseRef: "main", path: "", content: "", fileSha: "", message: "m",
            branch: "", title: "t", body: "", draft: false,
            files: [
                file("apps/a.yaml").proposal,
                StagedFile(path: "apps/new.yaml", original: "", fileSha: "", draft: "c: 3\n").proposal,
            ])
        let json = try JSONSerialization.jsonObject(with: Data(several.optionsJSON().utf8)) as? [String: Any]
        let files = json?["files"] as? [[String: Any]]
        XCTAssertEqual(files?.count, 2)
        XCTAssertEqual(files?.first?["path"] as? String, "apps/a.yaml")
        XCTAssertEqual(files?.first?["content"] as? String, "b: 2\n")
        XCTAssertEqual(files?.first?["fileSha"] as? String, "sha-apps/a.yaml")
        XCTAssertEqual(files?.last?["fileSha"] as? String, "")
    }

    func testTheResultReadsItsCommits() throws {
        let result = try TalosJSON.decode(
            GitProposalResult.self,
            from: #"{"url":"u","number":4,"branch":"b","commit":"c2","commits":["c1","c2"],"forge":"github"}"#)
        XCTAssertEqual(result.commits, ["c1", "c2"])
    }
}

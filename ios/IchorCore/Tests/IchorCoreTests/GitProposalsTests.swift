import XCTest
@testable import IchorCore

final class GitProposalsTests: XCTestCase {
    private func record(_ n: Int, tool: String = "argocd") -> GitProposalRecord {
        GitProposalRecord(
            tool: tool, kind: "Application", namespace: "argocd", name: "web",
            repoURL: "https://github.com/example-org/cluster-config", path: "apps/web/deployment.yaml",
            title: "Change \(n)", url: "https://github.com/example-org/cluster-config/pull/\(n)", number: n,
            createdAt: 1)
    }

    func testKeepsTheNewestFirstOnceAndAtMostFifty() {
        var list: [GitProposalRecord] = []
        for n in 1...(ProposalsStore.maxPerCluster + 5) { list = ProposalsStore.adding(record(n), to: list) }
        XCTAssertEqual(list.count, ProposalsStore.maxPerCluster)
        XCTAssertEqual(list.first?.number, ProposalsStore.maxPerCluster + 5)

        var again = record(30)
        again.title = "again"
        list = ProposalsStore.adding(again, to: list)
        XCTAssertEqual(list.first?.title, "again")
        XCTAssertEqual(list.filter { $0.number == 30 }.count, 1)
    }

    func testStoreRoundTripAndAndroidShape() throws {
        let saved = ["fp": [record(1), record(2, tool: "")]]
        XCTAssertEqual(ProposalsStore.decode(ProposalsStore.encode(saved)), saved)
        XCTAssertEqual(ProposalsStore.keep(saved, fingerprints: ["other"]), [:])
        // Android's record decodes (createdAt in milliseconds, absent fields defaulted).
        let android =
            #"{"tool":"flux","kind":"Kustomization","namespace":"flux-system","name":"apps","repoURL":"https://gitlab.com/example-org/x","number":3,"createdAt":1760860800000}"#
        let r = try JSONDecoder().decode(GitProposalRecord.self, from: Data(android.utf8))
        XCTAssertEqual(r.number, 3)
        XCTAssertEqual(r.host, "gitlab.com")
        XCTAssertTrue(r.isFor(tool: "flux", kind: "Kustomization", namespace: "flux-system", name: "apps"))
    }

    func testStatusFromTheForgeAndTheApp() {
        let merged = GitRequestState(state: "merged", mergeSha: "abc1234")
        XCTAssertEqual(proposalStatus(nil, nil), .unknown)
        XCTAssertEqual(proposalStatus(GitRequestState(state: "open"), nil), .open)
        XCTAssertEqual(proposalStatus(GitRequestState(state: "open", draft: true), nil), .draft)
        XCTAssertEqual(proposalStatus(GitRequestState(state: "closed"), nil), .closed)
        XCTAssertEqual(proposalStatus(merged, nil), .merged)
        XCTAssertEqual(proposalStatus(merged, RevisionRuns(runs: true, revision: "abc1234")), .deployed)
        XCTAssertEqual(proposalStatus(merged, RevisionRuns(revision: "def5678")), .otherRevision)
    }

    func testTheDemoHasOneOpenAndOneMerged() {
        XCTAssertEqual(ProposalsStore.demo.map(\.number), [1, 2])
        XCTAssertTrue(
            ProposalsStore.demo.allSatisfy {
                $0.isFor(tool: "argocd", kind: "Application", namespace: "argocd", name: "hello-ichor")
            })
    }
}

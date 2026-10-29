import XCTest
@testable import IchorCore

final class ProposalAlertsTests: XCTestCase {
    private let repo = "https://git.example.org/example-org/cluster-config"

    private func record(_ number: Int, state: String = "open", forge: String = "gitea", createdAt: Int64 = 1)
        -> GitProposalRecord
    {
        GitProposalRecord(
            tool: "argocd", kind: "Application", namespace: "argocd", name: "ingest", repoURL: repo, path: "a.yaml",
            title: "Raise ingest replicas", url: "", number: number, forge: forge, createdAt: createdAt, state: state)
    }

    func testOpenThenMergedOrClosedIsToldOnce() {
        let records = [record(12), record(13), record(14)]
        let states = [
            RequestStateItem(repoURL: repo, number: 12, state: "merged", mergedAt: "2026-10-22T10:00:00Z", sha: "m"),
            RequestStateItem(repoURL: repo, number: 13, state: "closed"),
            RequestStateItem(repoURL: repo, number: 14, state: "open"),
        ]
        let first = proposalStateChanges(records: records, states: states, now: 1000)
        XCTAssertEqual(first.changes.map(\.record.number), [12, 13])
        XCTAssertEqual(first.changes.map(\.state), ["merged", "closed"])
        XCTAssertEqual(first.changes.first?.alertKey, "proposal:\(repo)#12")
        XCTAssertEqual(first.changes.first?.alert.title, "Your pull request #12 was merged")
        XCTAssertEqual(first.records.map(\.state), ["merged", "closed", "open"])
        XCTAssertEqual(first.records[0].stateAt, 1000)
        XCTAssertEqual(first.records[2].stateAt, 0, "an unchanged state keeps its time")

        let again = proposalStateChanges(records: first.records, states: states, now: 2000)
        XCTAssertTrue(again.changes.isEmpty, "a merge is told once")
    }

    func testNeverReadIsABaselineAndErrorsChangeNothing() {
        let out = proposalStateChanges(
            records: [record(1, state: ""), record(2)],
            states: [
                RequestStateItem(repoURL: repo, number: 1, state: "merged"),
                RequestStateItem(repoURL: repo, number: 2, state: "", error: "429 Too Many Requests"),
            ], now: 5)
        XCTAssertTrue(out.changes.isEmpty)
        XCTAssertEqual(out.records.map(\.state), ["merged", "open"])
    }

    func testOnlyOpenNonDemoRecordsAreAskedByHostTwentyAtMost() {
        var records = (1...25).map { record($0, createdAt: Int64($0)) }
        records += [
            record(90, state: "merged"), record(91, forge: "demo"),
            GitProposalRecord(
                tool: "", repoURL: "https://github.com/example-org/other", path: "x", title: "t", url: "", number: 3),
        ]
        let asked = proposalsToCheck(records)
        XCTAssertEqual(asked["git.example.org"]?.count, 20)
        XCTAssertEqual(asked["git.example.org"]?.first?.number, 25, "newest first")
        XCTAssertEqual(asked["github.com"]?.map(\.number), [3])
        XCTAssertFalse(asked.values.joined().contains { $0.number == 90 || $0.number == 91 })
    }

    func testOldRecordsDecodeAndNewOnesStartOpen() throws {
        let old =
            #"[{"tool":"argocd","repoURL":"https://git.example.org/example-org/cluster-config","path":"a","title":"t","url":"","number":4,"createdAt":1}]"#
        let decoded = try JSONDecoder().decode([GitProposalRecord].self, from: Data(old.utf8))
        XCTAssertEqual(decoded.first?.state, "")
        XCTAssertEqual(decoded.first?.stateAt, 0)

        let added = ProposalsStore.adding(decoded[0], to: [], now: 77)
        XCTAssertEqual(added.first?.state, "open")
        XCTAssertEqual(added.first?.stateAt, 77)
        let encoded = try JSONEncoder().encode(added)
        XCTAssertTrue(String(decoding: encoded, as: UTF8.self).contains(#""state":"open""#))
    }

    func testBackupWritesFormat4OnlyWithProposals() throws {
        XCTAssertEqual(backupPayloadFormat(), 3)
        XCTAssertEqual(backupPayloadFormat(hasProposals: true), 4)
        XCTAssertNil(backupProposals([:], fingerprints: ["fp"]))
        XCTAssertNil(
            backupProposals(["gone": [record(1)]], fingerprints: ["fp"]), "a removed cluster's proposals stay out")

        let proposals = try XCTUnwrap(backupProposals(["fp": [record(1)], "gone": [record(2)]], fingerprints: ["fp"]))
        XCTAssertEqual(Array(proposals.keys), ["fp"])
        let payload = BackupPayload(
            format: backupPayloadFormat(hasProposals: true), platform: "ios", talosconfig: "context: lab\n",
            proposals: proposals)
        let round = try JSONDecoder().decode(BackupPayload.self, from: JSONEncoder().encode(payload))
        XCTAssertEqual(restoredProposals(round, fingerprints: ["fp"])?["fp"]?.first?.number, 1)

        var three = round
        three.format = 3
        XCTAssertNil(restoredProposals(three, fingerprints: ["fp"]), "only a format 4 backup carries proposals")
    }

    func testProposalAlertsAreGitOpsAlerts() {
        XCTAssertEqual(alertKind(key: "proposal:\(repo)#12"), "gitops")
    }
}

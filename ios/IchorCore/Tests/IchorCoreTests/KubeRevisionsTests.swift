import XCTest
@testable import IchorCore

final class KubeRevisionsTests: XCTestCase {
    func testStatefulSetHistoryDecoding() throws {
        let list = try TalosJSON.decode(
            WorkloadRevisionList.self,
            from: """
                {"kind":"StatefulSet","manual":true,"paused":false,
                 "revisions":[{"revision":7,"replicaSet":"db-7f8c9","created":1700000000000,
                               "images":["postgres:17"],"changeCause":"","replicas":0,"current":true},
                              {"revision":5,"replicaSet":"db-5a1b2","images":["postgres:16"]}]}
                """)
        XCTAssertEqual(list.kind, "StatefulSet")
        XCTAssertTrue(list.manual)
        XCTAssertFalse(list.paused)
        XCTAssertEqual(list.revisions.map(\.replicaSet), ["db-7f8c9", "db-5a1b2"])
        XCTAssertEqual(try TalosJSON.decode(WorkloadRevisionList.self, from: "{}"), WorkloadRevisionList())
    }

    func testDiffAndItsImagesSummary() throws {
        let diff = try TalosJSON.decode(
            RevisionDiff.self,
            from: """
                {"diff":"--- revision 7 (current)\\n+++ revision 5\\n","from":"revision 7 (current)","to":"revision 5",
                 "fromRevision":7,"toRevision":5,
                 "images":{"from":["postgres:17","exporter:1"],"to":["postgres:16","exporter:1"]}}
                """)
        XCTAssertEqual(diff.toRevision, 5)
        XCTAssertEqual(diff.images.changes, [ImageChange(from: "postgres:17", to: "postgres:16")])
        XCTAssertEqual(
            RevisionImages(from: ["app:2", "sidecar:1"], to: ["app:2"]).changes,
            [ImageChange(from: "sidecar:1", to: "")], "a container only one side has")
        XCTAssertEqual(RevisionImages(from: ["app:2"], to: ["app:2"]).changes, [])
        XCTAssertEqual(try TalosJSON.decode(RevisionDiff.self, from: #"{"images":null}"#).images, RevisionImages())
    }

    func testPausedDeploymentResumes() {
        let paused = KubeWorkload(kind: "Deployment", namespace: "demo-shop", name: "web", state: "paused")
        XCTAssertTrue(paused.rolloutPaused)
        XCTAssertFalse(KubeWorkload(kind: "Deployment", namespace: "demo-shop", name: "web").rolloutPaused)
    }
}

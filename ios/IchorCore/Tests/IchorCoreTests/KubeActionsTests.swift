import XCTest
@testable import IchorCore

final class KubeActionsTests: XCTestCase {
    func testScaleAndHistoryKinds() {
        XCTAssertTrue(KubeWorkload(kind: "Deployment", namespace: "a", name: "b").canScale)
        XCTAssertTrue(KubeWorkload(kind: "StatefulSet", namespace: "a", name: "b").canScale)
        XCTAssertFalse(KubeWorkload(kind: "DaemonSet", namespace: "a", name: "b").canScale)
        XCTAssertTrue(KubeWorkload(kind: "Deployment", namespace: "a", name: "b").hasHistory)
        XCTAssertFalse(KubeWorkload(kind: "StatefulSet", namespace: "a", name: "b").hasHistory)
        XCTAssertTrue(scaleNeedsTypedName(0))
        XCTAssertFalse(scaleNeedsTypedName(1))
    }

    func testRevisionsDecoding() throws {
        let list = try TalosJSON.decode(DeploymentRevisionList.self, from: """
        {"revisions":[{"revision":3,"replicaSet":"web-7d9c6","created":1700000000000,"images":["nginx:1.27"],
                       "changeCause":"bump nginx","replicas":2,"current":true},
                      {"revision":2,"replicaSet":"web-5f4b8","images":null}]}
        """)
        XCTAssertEqual(list.revisions.map(\.revision), [3, 2])
        XCTAssertEqual(list.revisions[0], DeploymentRevision(revision: 3, replicaSet: "web-7d9c6", created: 1_700_000_000_000,
                                                             images: ["nginx:1.27"], changeCause: "bump nginx", replicas: 2,
                                                             current: true))
        XCTAssertEqual(list.revisions[1].images, [])
        XCTAssertFalse(list.revisions[1].current)
        XCTAssertEqual(try TalosJSON.decode(DeploymentRevisionList.self, from: #"{"revisions":null}"#).revisions, [])
    }

    func testPodLogContainerChoices() {
        XCTAssertEqual(podLogContainerChoices(fromError: "a container name must be specified for pod web-1, choose one of: [app sidecar]"),
                       ["app", "sidecar"])
        XCTAssertEqual(podLogContainerChoices(fromError: "a container name must be specified for pod w, choose one of: [app] or one of the init containers: [init]"),
                       ["app"])
        XCTAssertEqual(podLogContainerChoices(fromError: "pods \"web-1\" not found"), [])
        XCTAssertEqual(podLogContainerChoices(fromError: "choose one of: [unterminated"), [])
    }

    func testPodContainersDecoding() throws {
        let pod = try TalosJSON.decode(KubePod.self, from: """
        {"namespace":"web","name":"api-1","containerNames":["app","sidecar"],"lastTermination":"OOMKilled (exit 137)"}
        """)
        XCTAssertEqual(pod.containerNames, ["app", "sidecar"])
        XCTAssertEqual(pod.lastTermination, "OOMKilled (exit 137)")
        let old = try TalosJSON.decode(KubePod.self, from: #"{"namespace":"web","name":"a","containerNames":null}"#)
        XCTAssertEqual(old.containerNames, [])
        XCTAssertEqual(old.lastTermination, "")
    }

    func testPodLogLines() {
        XCTAssertEqual(podLogLines("a\nb\n"), ["a", "b"])
        XCTAssertEqual(podLogLines("a\n\nb"), ["a", "", "b"])
        XCTAssertEqual(podLogLines(""), [])
    }
}

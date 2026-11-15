import XCTest
@testable import IchorCore

final class KubeTopTests: XCTestCase {
    func testDecodesTheCoreJSON() throws {
        let json =
            #"{"available":true,"forbidden":false,"pods":[{"namespace":"web","name":"api","node":"w1","cpu":0.125,"memory":230686720,"cpuRequest":0.3,"cpuLimit":1.1,"memoryRequest":335544320,"memoryLimit":671088640}]}"#
        let top = try JSONDecoder().decode(KubeTopPods.self, from: Data(json.utf8))
        XCTAssertTrue(top.available)
        XCTAssertFalse(top.boundsRead)
        XCTAssertTrue(try JSONDecoder().decode(KubeTopPods.self, from: Data(#"{"boundsRead":true}"#.utf8)).boundsRead)
        XCTAssertEqual(top.pods.first?.id, "web/api")
        XCTAssertEqual(top.byKey["web/api"]?.cpu ?? 0, 0.125, accuracy: 1e-9)
    }

    func testDecodesAPodWithItsContainers() throws {
        let json =
            #"{"available":true,"boundsRead":true,"pods":[{"namespace":"web","name":"api","cpu":0.125,"memory":230686720,"containers":[{"name":"app","cpu":0.12,"memory":209715200,"cpuRequest":0.25,"cpuLimit":1,"memoryRequest":268435456,"memoryLimit":0},{"name":"proxy","cpu":0.005,"memory":20971520}]}]}"#
        let pod = try XCTUnwrap(try JSONDecoder().decode(KubeTopPods.self, from: Data(json.utf8)).pods.first)
        XCTAssertEqual(pod.containers.map(\.name), ["app", "proxy"])
        let app = try XCTUnwrap(pod.containers.first)
        XCTAssertEqual(app.cpuFraction ?? -1, 0.12, accuracy: 1e-9)
        // No memory limit: against its own request.
        XCTAssertEqual(app.memoryBound, 268_435_456)
        XCTAssertEqual(app.memoryFraction ?? -1, 0.78125, accuracy: 1e-9)
        // Neither bound: no bar.
        XCTAssertNil(pod.containers.last?.cpuFraction)
        // An older core sends no containers: an empty list.
        let older = try JSONDecoder().decode(KubeTopPod.self, from: Data(#"{"name":"api"}"#.utf8))
        XCTAssertTrue(older.containers.isEmpty)
    }

    func testOlderOrEmptyAnswersDecode() throws {
        let top = try JSONDecoder().decode(KubeTopNodes.self, from: Data(#"{"forbidden":true}"#.utf8))
        XCTAssertFalse(top.available)
        XCTAssertTrue(top.forbidden)
        XCTAssertTrue(top.nodes.isEmpty)
    }

    func testABarIsMeasuredAgainstTheLimitElseTheRequest() {
        let pod = KubeTopPod(
            namespace: "a", name: "p", cpu: 0.5, memory: 300, cpuRequest: 0.25, cpuLimit: 1, memoryRequest: 200)
        XCTAssertEqual(pod.cpuFraction ?? -1, 0.5, accuracy: 1e-9)
        XCTAssertEqual(pod.memoryFraction ?? -1, 1, accuracy: 1e-9)
        XCTAssertNil(KubeTopPod(namespace: "a", name: "q", cpu: 0.1).cpuFraction)
    }

    func testCPUReadsLikeKubectl() {
        XCTAssertEqual(formatCPU(0), "0m")
        XCTAssertEqual(formatCPU(0.125), "125m")
        XCTAssertEqual(formatCPU(0.9994), "999m")
        XCTAssertEqual(formatCPU(1.5), "1.5")
        XCTAssertEqual(formatCPU(12), "12")
    }

    func testSortsBusiestFirstAndUnmeasuredLast() {
        let usage = [
            "idle": KubeTopPod(namespace: "a", name: "idle", cpu: 0.01, memory: 900),
            "busy": KubeTopPod(namespace: "a", name: "busy", cpu: 2, memory: 10),
        ]
        let rows = ["new", "idle", "busy"]
        XCTAssertEqual(rows.sortedByUsage(.cpu) { usage[$0] }, ["busy", "idle", "new"])
        XCTAssertEqual(rows.sortedByUsage(.memory) { usage[$0] }, ["idle", "busy", "new"])
        XCTAssertEqual(rows.sortedByUsage(.name) { usage[$0] }, rows)
    }
}

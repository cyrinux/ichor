import XCTest
@testable import IchorCore

final class KubeTopTests: XCTestCase {
    func testDecodesTheCoreJSON() throws {
        let json = #"{"available":true,"forbidden":false,"pods":[{"namespace":"web","name":"api","node":"w1","cpu":0.125,"memory":230686720,"cpuRequest":0.3,"cpuLimit":1.1,"memoryRequest":335544320,"memoryLimit":671088640}]}"#
        let top = try JSONDecoder().decode(KubeTopPods.self, from: Data(json.utf8))
        XCTAssertTrue(top.available)
        XCTAssertFalse(top.boundsRead)
        XCTAssertTrue(try JSONDecoder().decode(KubeTopPods.self, from: Data(#"{"boundsRead":true}"#.utf8)).boundsRead)
        XCTAssertEqual(top.pods.first?.id, "web/api")
        XCTAssertEqual(top.byKey["web/api"]?.cpu ?? 0, 0.125, accuracy: 1e-9)
    }

    func testOlderOrEmptyAnswersDecode() throws {
        let top = try JSONDecoder().decode(KubeTopNodes.self, from: Data(#"{"forbidden":true}"#.utf8))
        XCTAssertFalse(top.available)
        XCTAssertTrue(top.forbidden)
        XCTAssertTrue(top.nodes.isEmpty)
    }

    func testABarIsMeasuredAgainstTheLimitElseTheRequest() {
        let pod = KubeTopPod(namespace: "a", name: "p", cpu: 0.5, memory: 300, cpuRequest: 0.25, cpuLimit: 1, memoryRequest: 200)
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

import Foundation
import XCTest
@testable import IchorCore

final class SelectedPodsTests: XCTestCase {
    func testPhaseQueriesAreWhatTheCoreTakes() {
        XCTAssertEqual(PodPhaseFilter.allCases.map(\.query), ["", "Running", "!Succeeded"])
    }

    func testANodeListsEveryNamespaceAWorkloadEveryNode() {
        let node = PodSelection.node("192.0.2.20")
        let kubeNode = PodSelection.kubeNode("worker-1")
        let workload = PodSelection.workload(kind: "Deployment", namespace: "demo", name: "web")
        XCTAssertTrue(node.showsNamespace)
        XCTAssertFalse(node.showsNode)
        XCTAssertTrue(kubeNode.showsNamespace)
        XCTAssertFalse(kubeNode.showsNode)
        XCTAssertFalse(workload.showsNamespace)
        XCTAssertTrue(workload.showsNode)
    }

    func testOnlyTheSelectorKindsListTheirPods() {
        XCTAssertEqual(KubeWorkload(kind: "StatefulSet", namespace: "db", name: "postgres").podSelection,
                       .workload(kind: "StatefulSet", namespace: "db", name: "postgres"))
        XCTAssertNil(KubeWorkload(kind: "CronJob", namespace: "db", name: "backup").podSelection)
    }

    func testTheFirstPageLoadsThenTheNextOnScroll() throws {
        let size = selectedPodsPageSize
        let pages: [String: KubePage<String>] = [
            "": KubePage(items: (0..<size).map { "a\($0)" }, continueToken: "t1", complete: false),
            "t1": KubePage(items: ["b"]),
        ]
        let (first, all) = try runBlocking {
            let first = try await loadPages(cap: size, fetch: { pages[$0]! })
            return (first, try await first.loadingMore { pages[$0]! })
        }
        XCTAssertTrue(first.hasMore)
        XCTAssertEqual(first.items.count, size)
        XCTAssertTrue(all.done)
        XCTAssertEqual(all.items.last, "b")
    }
}

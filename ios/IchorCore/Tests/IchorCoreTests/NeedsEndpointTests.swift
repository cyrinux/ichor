import Foundation
import XCTest
@testable import IchorCore

final class NeedsEndpointTests: XCTestCase {
    func testTalosContextWithoutEndpointsNeedsOne() throws {
        // The core sends `null` for a context generated without endpoints.
        let json = Data(#"{"name":"lab","endpoints":null,"nodes":null,"roles":["os:admin"],"certNotAfter":0}"#.utf8)
        let summary = try JSONDecoder().decode(ContextSummary.self, from: json)
        XCTAssertTrue(summary.needsEndpoint)
    }

    func testOthersDoNot() {
        XCTAssertFalse(ContextSummary(name: "lab", endpoints: ["10.0.0.1"]).needsEndpoint)
        XCTAssertFalse(ContextSummary(name: "demo", demo: true).needsEndpoint)
        XCTAssertFalse(ContextSummary(name: "kube", kind: ContextKind.kube).needsEndpoint)
    }
}

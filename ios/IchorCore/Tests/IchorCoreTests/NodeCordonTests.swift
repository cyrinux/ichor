import XCTest
@testable import IchorCore

final class NodeCordonTests: XCTestCase {
    func testCordonDecodingAndAttention() throws {
        let node = try TalosJSON.decode(NodeOverview.self, from: """
        {"node":"10.0.0.5","hostname":"w-2","reachable":true,"version":"v1.12.0","arch":"amd64","platform":"metal",
         "role":"worker","stage":"running","ready":true,"unmetConditions":[],"cordoned":true,"cordonKnown":true}
        """)
        XCTAssertTrue(node.cordoned)
        XCTAssertTrue(node.cordonKnown)
        XCTAssertTrue(node.needsAttention)

        let older = try TalosJSON.decode(NodeOverview.self, from: """
        {"node":"10.0.0.6","hostname":"w-3","reachable":true,"version":"v1.12.0","arch":"amd64","platform":"metal",
         "role":"worker","stage":"running","ready":true,"unmetConditions":null}
        """)
        XCTAssertFalse(older.cordoned)
        XCTAssertFalse(older.cordonKnown)
        XCTAssertFalse(older.needsAttention)
    }
}

import XCTest
@testable import IchorCore

final class NodeDiscoveryTests: XCTestCase {
    private let json = """
    {"context":"prod","nodes":[
      {"address":"10.0.0.1","addresses":["10.0.0.1"],"hostname":"cp-1","role":"controlplane","known":true},
      {"address":"10.0.0.11","addresses":["fe80::1","10.0.0.11"],"hostname":"worker-a","role":"worker","known":false},
      {"address":"","addresses":null,"hostname":"ghost","role":"worker","known":false},
      {"address":"10.0.0.12","addresses":["10.0.0.12"],"hostname":"worker-b","role":"worker","known":false}
    ]}
    """

    private func discovery() throws -> NodeDiscovery { try TalosJSON.decode(NodeDiscovery.self, from: json) }

    func testMissingSkipsKnownMembersAndThoseWithoutAddress() throws {
        XCTAssertEqual(try discovery().missing.map(\.hostname), ["worker-a", "worker-b"])
    }

    func testDismissedMembersAreNotOfferedAgain() throws {
        XCTAssertEqual(try discovery().offer(dismissed: ["10.0.0.11"]).map(\.address), ["10.0.0.12"])
    }

    func testNullNodesDecodeAsNone() throws {
        let empty = try TalosJSON.decode(NodeDiscovery.self, from: #"{"context":"prod","nodes":null}"#)
        XCTAssertEqual(empty.offer(dismissed: []), [])
    }
}

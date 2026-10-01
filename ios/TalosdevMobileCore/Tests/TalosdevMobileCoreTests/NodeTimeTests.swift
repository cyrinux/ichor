import XCTest
@testable import TalosdevMobileCore

final class NodeTimeTests: XCTestCase {
    func testDecodeClusterTime() throws {
        let json = #"{"context":"prod","nodes":[{"node":"10.0.0.2","server":"time.cloudflare.com","localTime":1000,"remoteTime":1600,"offsetMs":600},{"node":"10.0.0.3","server":"","localTime":0,"remoteTime":0,"offsetMs":0,"error":"connection refused"}]}"#
        let time = try TalosJSON.decode(ClusterTimeInfo.self, from: json)
        XCTAssertEqual(time.nodes[0].offsetMs, 600)
        XCTAssertNil(time.nodes[0].error)
        XCTAssertEqual(time.nodes[0].drift, .warning)
        XCTAssertEqual(time.nodes[1].drift, .bad)
        XCTAssertEqual(time.worst, .bad)
    }

    func testThresholds() {
        XCTAssertEqual(timeDrift(offsetMs: 0), .ok)
        XCTAssertEqual(timeDrift(offsetMs: 499), .ok)
        XCTAssertEqual(timeDrift(offsetMs: -499), .ok)
        XCTAssertEqual(timeDrift(offsetMs: 500), .warning)
        XCTAssertEqual(timeDrift(offsetMs: -4_999), .warning)
        XCTAssertEqual(timeDrift(offsetMs: 5_000), .bad)
        XCTAssertEqual(timeDrift(offsetMs: -5_000), .bad)
        XCTAssertEqual(timeDrift(offsetMs: Int64.min), .bad)
        XCTAssertEqual(timeDrift(offsetMs: 3, failed: true), .bad)
        XCTAssertEqual(ClusterTimeInfo(context: "x", nodes: []).worst, .ok)
    }

    func testFormatOffset() {
        XCTAssertEqual(formatOffset(12), "+12 ms")
        XCTAssertEqual(formatOffset(-12), "-12 ms")
        XCTAssertEqual(formatOffset(1_250), "+1.25 s")
        XCTAssertEqual(formatOffset(-125_000), "-2 min 5 s")
    }
}

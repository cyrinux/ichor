import XCTest
@testable import IchorCore

final class DataStoreStatusTests: XCTestCase {
    func testSavingShowsNoProblem() throws {
        let status = try TalosJSON.decode(DataStoreStatus.self, from: #"{"persisting":true,"dir":"core","reason":""}"#)
        XCTAssertEqual(status, DataStoreStatus(persisting: true, dir: "core"))
        XCTAssertNil(status.problem)
    }

    func testNotSavingSaysWhy() throws {
        let status = try TalosJSON.decode(DataStoreStatus.self, from: #"{"persisting":false,"dir":"","reason":"data key must be 32 bytes, got 16"}"#)
        XCTAssertEqual(status.problem, "data key must be 32 bytes, got 16")
        XCTAssertEqual(DataStoreStatus(persisting: false).problem, "no data directory")
    }
}

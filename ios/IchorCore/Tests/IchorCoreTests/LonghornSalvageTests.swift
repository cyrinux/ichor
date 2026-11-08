import XCTest
@testable import IchorCore
final class LonghornSalvageTests: XCTestCase {
    func testExplicitEligibleSelection() throws {
        let raw =
            #"{"namespace":"storage","name":"vol","state":"detached","robustness":"faulted","canSalvage":true,"error":"","replicas":[{"name":"r1","node":"n1","healthyAt":1,"failedAt":2,"eligible":true,"blocked":""},{"name":"r2","node":"n2","healthyAt":0,"failedAt":0,"eligible":false,"blocked":"node-not-ready"}]}"#
        let p = try JSONDecoder().decode(LonghornSalvagePreview.self, from: Data(raw.utf8))
        XCTAssertEqual(p.replicas.map(\.blocked), ["", "node-not-ready"])
        XCTAssertFalse(p.accepts([]))
        XCTAssertTrue(p.accepts(["r1"]))
        XCTAssertFalse(p.accepts(["r2"]))
        XCTAssertFalse(p.accepts(["foreign"]))
    }
}

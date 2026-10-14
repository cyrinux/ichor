import XCTest
@testable import IchorCore
final class LonghornAttachmentTests: XCTestCase {
    func testExplicitReadyNodeAndFreshDetachNode() throws {
        let raw = #"{"namespace":"storage","name":"vol","state":"detached","node":"","canAttach":true,"canDetach":false,"error":"","nodes":[{"name":"n1","eligible":true},{"name":"n2","eligible":false}]}"#
        let p = try JSONDecoder().decode(LonghornAttachmentPreview.self, from: Data(raw.utf8))
        XCTAssertFalse(p.accepts("", attach: true)); XCTAssertTrue(p.accepts("n1", attach: true)); XCTAssertFalse(p.accepts("n2", attach: true)); XCTAssertFalse(p.accepts("foreign", attach: true)); XCTAssertFalse(p.accepts("n1", attach: false))
        let attached = LonghornAttachmentPreview(namespace: "storage", name: "vol", state: "attached", node: "n1", canAttach: false, canDetach: true, error: "", nodes: [])
        XCTAssertTrue(attached.accepts("n1", attach: false)); XCTAssertFalse(attached.accepts("n2", attach: false))
    }
}

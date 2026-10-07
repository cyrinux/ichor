import XCTest
@testable import IchorCore

final class HelmRollbackTests: XCTestCase {
    func testDecodesPlan() throws {
        let plan = try TalosJSON.decode(HelmRollbackPlan.self, from: """
        {"namespace":"apps","name":"site","from":3,"to":2,"fromChart":"site-1.1.0","toChart":"site-1.0.0",
         "fromAppVersion":"1.1","toAppVersion":"1.0","future":true,
         "changes":[{"action":"update","kind":"Deployment","namespace":"apps","name":"web","error":""},
                    {"action":"delete","kind":"ClusterRole","name":"site","error":"forbidden","extra":1},
                    {"action":"keep","kind":"PersistentVolumeClaim","namespace":"apps","name":"data"}],
         "unchanged":4,"fluxOwner":"flux-system/site","blockers":[]}
        """)
        XCTAssertEqual(plan.from, 3)
        XCTAssertEqual(plan.to, 2)
        XCTAssertEqual(plan.toChart, "site-1.0.0")
        XCTAssertEqual(plan.toAppVersion, "1.0")
        XCTAssertEqual(plan.changes.count, 3)
        XCTAssertEqual(plan.changes[0].label, "Deployment apps/web")
        XCTAssertEqual(plan.changes[1].label, "ClusterRole site")
        XCTAssertEqual(plan.changes[1].error, "forbidden")
        XCTAssertEqual(plan.changes[2].error, "")
        XCTAssertEqual(Set(plan.changes.map(\.id)).count, 3)
        XCTAssertEqual(plan.unchanged, 4)
        XCTAssertEqual(plan.fluxOwner, "flux-system/site")
        XCTAssertTrue(plan.canRun)
        XCTAssertTrue(plan.keepsObjects)
    }

    func testBlockersStopIt() throws {
        let plan = try TalosJSON.decode(HelmRollbackPlan.self, from: #"{"blockers":["revision 9 not found"],"changes":null}"#)
        XCTAssertFalse(plan.canRun)
        XCTAssertEqual(plan.blockers, ["revision 9 not found"])
        XCTAssertEqual(plan.changes, [])
    }

    func testDecodesEmptyObject() throws {
        let plan = try TalosJSON.decode(HelmRollbackPlan.self, from: "{}")
        XCTAssertEqual(plan, HelmRollbackPlan())
        XCTAssertTrue(plan.canRun)
        XCTAssertFalse(plan.keepsObjects)
        XCTAssertEqual(try TalosJSON.decode(HelmRollbackChange.self, from: "{}"), HelmRollbackChange(action: "", kind: "", name: ""))
    }
}

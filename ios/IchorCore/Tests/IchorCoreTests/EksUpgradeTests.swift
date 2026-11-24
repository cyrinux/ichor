import XCTest
@testable import IchorCore

final class EksUpgradeTests: XCTestCase {
    private func plan(_ json: String) throws -> EksUpgradePlan {
        try JSONDecoder().decode(EksUpgradePlan.self, from: Data(json.utf8))
    }

    /// What go/ichorgo demoEKSUpgradePlan() answers: one minor available, a group behind.
    func testDecodesTheDemoPlan() throws {
        let p = try plan(
            """
            {"installed":true,"cluster":"ichor-demo","region":"eu-west-1","version":"1.34","status":"ACTIVE",
             "platformVersion":"eks.9","next":"1.35","nextKnown":true,"supportEnds":1812775585268,"updating":null,
             "groupsBehind":["gpu"],"groups":["gpu","system"],
             "blockers":["node groups are behind the current version: upgrade them first"],"error":"","forbidden":""}
            """)
        XCTAssertTrue(p.installed)
        XCTAssertNil(p.updating)
        XCTAssertEqual(p.supportEnds, 1_812_775_585_268)
        XCTAssertEqual(p.groupsBehind, ["gpu"])
        XCTAssertFalse(p.canUpgrade)
        XCTAssertEqual(p.state(update: nil), .available)
    }

    func testMissingFieldsTakeTheirDefaults() throws {
        let p = try plan("{}")
        XCTAssertFalse(p.installed)
        XCTAssertEqual(p.blockers, [])
        XCTAssertFalse(p.canUpgrade)
        XCTAssertEqual(p.state(update: nil), .unknown)
    }

    func testStates() {
        XCTAssertEqual(EksUpgradePlan(version: "1.35").state(update: nil), .upToDate)
        XCTAssertEqual(EksUpgradePlan(version: "1.34", next: "1.35").state(update: nil), .unchecked)
        XCTAssertTrue(EksUpgradePlan(version: "1.34", next: "1.35").canUpgrade)

        let running = NodeGroupUpdate(id: "c-1", status: NodeGroupUpdate.inProgressStatus, type: "VersionUpdate")
        let plan = EksUpgradePlan(version: "1.34", next: "1.35", nextKnown: true, updating: running)
        XCTAssertEqual(plan.state(update: nil), .running)
        XCTAssertFalse(plan.canUpgrade)

        // The update the sheet follows wins over the plan's.
        let done = NodeGroupUpdate(id: "c-1", status: NodeGroupUpdate.successfulStatus, type: "VersionUpdate")
        XCTAssertEqual(plan.state(update: done), .available)
    }

    func testTheReportCarriesThePlanThroughItsFilter() {
        let report = AutoscalingReport().withNodeGroups(
            NodeGroupsReport(installed: true), eksUpgrade: EksUpgradePlan(installed: true, version: "1.34"))
        XCTAssertEqual(report.filtered(namespace: nil, query: "x").eksUpgrade.version, "1.34")
    }
}

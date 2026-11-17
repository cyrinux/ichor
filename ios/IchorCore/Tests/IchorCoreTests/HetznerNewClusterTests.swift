import XCTest

@testable import IchorCore

final class HetznerNewClusterTests: XCTestCase {
    func testCostDecodes() throws {
        let cost = try TalosJSON.decode(
            HetznerNewClusterCost.self,
            from:
                #"{"currency":"EUR","serverHourly":"0.0060","serverMonthly":"3.49","primaryIPMonthly":"0.50","totalMonthly":"3.99"}"#
        )
        XCTAssertEqual(cost.totalMonthly, "3.99")
        XCTAssertEqual(cost.primaryIPMonthly, "0.50")
        XCTAssertEqual(try TalosJSON.decode(HetznerNewClusterCost.self, from: "{}").currency, "")
    }

    func testSpecEncodesTheCoreKeys() throws {
        let spec = HetznerNewClusterSpec(name: "lab", serverType: "cx23", location: "fsn1", snapshotId: 3)
        let object = try XCTUnwrap(
            JSONSerialization.jsonObject(with: JSONEncoder().encode(spec)) as? [String: Any])
        XCTAssertEqual(object["snapshotId"] as? Int, 3)
        XCTAssertEqual(object["allowSchedulingOnControlPlanes"] as? Bool, true)
        XCTAssertNil(object["token"])
    }

    func testSecretsEventCarriesTheTalosconfigOnlyUntilDropped() throws {
        let event = try TalosJSON.decode(
            JoinProgress.self, from: #"{"phase":"secrets","message":"ready","talosconfig":"context: lab"}"#)
        XCTAssertEqual(event.talosconfig, "context: lab")
        XCTAssertEqual(event.withoutTalosconfig.talosconfig, "")
        XCTAssertEqual(event.withoutTalosconfig.phase, "secrets")
        XCTAssertFalse(event.description.contains("context: lab"))
    }

    func testRequestFollowsTheHetznerClusterRun() {
        let request = JoinRequest(
            address: "", template: "", role: "controlplane", hostname: "lab", disk: "", staticAddress: "",
            hetznerCluster: HetznerNewClusterSpec(name: "lab", serverType: "cx23", location: "fsn1", snapshotId: 3))
        XCTAssertEqual(request.phases.first, .ip)
        XCTAssertEqual(request.phases.last, .ready)
        XCTAssertEqual(request.newClusterName, "lab")
        XCTAssertTrue(request.cloud)
        XCTAssertEqual(request.withReplacing(true).hetznerCluster, request.hetznerCluster)
    }
}

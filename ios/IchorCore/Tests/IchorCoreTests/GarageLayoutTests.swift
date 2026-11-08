import XCTest
@testable import IchorCore

final class GarageLayoutTests: XCTestCase {
    private let a = String(repeating: "a", count: 64)
    private let c = String(repeating: "c", count: 64)
    private let d = String(repeating: "d", count: 64)

    // Shaped like KubeGarageLayout's answer, with a field the app does not know.
    private var layoutJSON: String {
        #"""
        {"version":12,"nextVersion":13,"partitionSize":1835008000,"zoneRedundancy":"maximum","stagedZoneRedundancy":"2","futureField":true,
         "preview":{"ok":true,"error":"","messages":["dc3: 256 partitions moved in"],"movedPartitions":256,"usableCapacity":3000,"effectiveCapacity":1000,"totalCapacity":4000,"lowUsableCapacity":true,"lowPartitionSize":false},
         "nodes":[
          {"id":"\#(a)","hostname":"garage-a","kubeNode":"w1","up":false,"known":true,"draining":false,"dataTotal":1100,"role":{"zone":"dc1","capacity":1000,"tags":["a"],"storedPartitions":256,"usableCapacity":900},"staged":"none","stagedRole":null},
          {"id":"\#(String(repeating: "b", count: 64))","hostname":"","up":true,"known":true,"role":{"zone":"dc2","capacity":1000,"tags":[]},"staged":"none"},
          {"id":"\#(c)","hostname":"garage-c","up":true,"known":true,"role":{"zone":"dc2","capacity":-1,"tags":["gw"]},"staged":"remove","stagedRole":null},
          {"id":"\#(d)","hostname":"garage-d","up":true,"known":true,"dataTotal":2200,"role":null,"staged":"add","stagedRole":{"zone":"dc3","capacity":2000,"tags":["d"]}}],
         "versions":[{"version":12,"status":"current","storageNodes":2,"gatewayNodes":1},{"version":11,"status":"draining","storageNodes":2,"gatewayNodes":1},{"version":10,"status":"someday","storageNodes":1,"gatewayNodes":0}],
         "minAck":11,"trackers":[{"id":"\#(a)","hostname":"garage-a","ack":11,"sync":11,"syncAck":11}],"stuck":true,"messages":[]}
        """#
    }

    func testDecodesTheLayout() throws {
        let layout = try TalosJSON.decode(GarageLayout.self, from: layoutJSON)
        XCTAssertEqual(layout.version, 12)
        XCTAssertTrue(layout.hasStaged)
        XCTAssertEqual(layout.stagedZoneRedundancy, "2")
        XCTAssertEqual(layout.preview?.movedPartitions, 256)
        XCTAssertEqual(layout.preview?.lowUsableCapacity, true)

        XCTAssertEqual(layout.members.map(\.label), ["garage-a", String(repeating: "b", count: 16), "garage-c"])
        XCTAssertEqual(layout.storageNodes, 2)
        XCTAssertEqual(layout.gatewayNodes, 1)
        XCTAssertEqual(layout.staged.map(\.label), ["garage-c", "garage-d"])
        XCTAssertTrue(layout.candidates.isEmpty)
        XCTAssertEqual(layout.zones, ["dc1", "dc2", "dc3"])

        let gateway = try XCTUnwrap(layout.node(c))
        XCTAssertEqual(gateway.role?.gateway, true)
        XCTAssertEqual(gateway.staged, .remove)
        XCTAssertNil(gateway.effectiveRole)

        let joining = try XCTUnwrap(layout.node(d))
        XCTAssertEqual(joining.staged, .add)
        XCTAssertEqual(joining.effectiveRole?.zone, "dc3")
        XCTAssertEqual(layout.node(a)?.role?.usableFraction ?? 0, 0.9, accuracy: 0.001)

        XCTAssertEqual(layout.versions.map(\.status), [.current, .draining, .unknown])
        XCTAssertTrue(layout.stuck)
        XCTAssertEqual(layout.trackers.first?.label, "garage-a")
    }

    func testCapacityConversions() {
        XCTAssertEqual(garageCapacityBytes("1", unit: .tib), 1 << 40)
        XCTAssertEqual(garageCapacityBytes("1,5", unit: .tib), Int64(1.5 * Double(1 << 40)))
        XCTAssertEqual(garageCapacityBytes(" 450 ", unit: .gib), 450 << 30)
        XCTAssertNil(garageCapacityBytes("", unit: .gib))
        XCTAssertNil(garageCapacityBytes("0", unit: .gib))
        XCTAssertNil(garageCapacityBytes("-2", unit: .gib))
        XCTAssertNil(garageCapacityBytes("big", unit: .gib))

        XCTAssertEqual(garageCapacityText(Int64(1.5 * Double(1 << 40)), unit: .tib), "1.5")
        XCTAssertEqual(garageCapacityText(450 << 30, unit: .gib), "450")
        XCTAssertEqual(GarageCapacityUnit.forBytes(1 << 40), .tib)
        XCTAssertEqual(GarageCapacityUnit.forBytes(500 << 30), .gib)
    }

    func testSuggestedCapacityStaysBelowTheDisk() {
        XCTAssertEqual(garageSuggestedCapacity(dataTotal: 500 << 30), 499 << 30)
        XCTAssertEqual(garageSuggestedCapacity(dataTotal: (1 << 40) + (200 << 30)), 1 << 40)
        XCTAssertEqual(garageSuggestedCapacity(dataTotal: 2 << 40), 1 << 40)
        XCTAssertEqual(garageSuggestedCapacity(dataTotal: 0), 0)
        XCTAssertEqual(garageSuggestedCapacity(dataTotal: 1 << 30), 1 << 30)
    }
}

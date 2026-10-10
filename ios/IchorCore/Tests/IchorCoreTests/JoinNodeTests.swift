import XCTest
@testable import IchorCore

final class JoinNodeTests: XCTestCase {
    func testInspectionDecodes() throws {
        let node = try TalosJSON.decode(MaintenanceInspection.self, from: """
        {"address":"192.168.1.42","version":"v1.14.2","arch":"amd64","platform":"metal",
         "system":{"manufacturer":"Sample Systems","product":"Box 1","version":"","serial":"S-0001","uuid":"","sku":"","biosVersion":""},
         "disks":[{"name":"nvme0n1","devPath":"/dev/nvme0n1","model":"Sample NVMe","serial":"","size":549755813888,
                   "type":"nvme","wwid":"","busPath":"","systemDisk":false,"readonly":false}],
         "links":[{"name":"enp1s0","type":"ether","kind":"","state":"up","hardwareAddr":"02:00:00:00:00:42","mtu":1500,"speedMbit":1000,"virtual":false},
                  {"name":"enp2s0","type":"ether","kind":"","state":"down","hardwareAddr":"02:00:00:00:00:43","mtu":1500,"speedMbit":0,"virtual":false}],
         "addresses":[{"address":"192.168.1.42/24","link":"enp1s0","family":"inet4","scope":"global","virtual":false},
                      {"address":"fd00::42/64","link":"enp1s0","family":"inet6","scope":"global","virtual":false}],
         "maintenance":true,"errors":{}}
        """)
        XCTAssertTrue(node.maintenance)
        XCTAssertEqual(node.version, "v1.14.2")
        XCTAssertEqual(node.system?.manufacturer, "Sample Systems")
        XCTAssertEqual(node.disks.first?.size, 549_755_813_888)
        XCTAssertEqual(node.addresses(on: "enp1s0"), ["192.168.1.42/24", "fd00::42/64"])
        XCTAssertEqual(node.addresses(on: "enp2s0"), [])
    }

    func testInstalledNodeWithNulls() throws {
        let node = try TalosJSON.decode(MaintenanceInspection.self, from: """
        {"address":"192.168.1.10","version":"","arch":"","platform":"","system":null,"disks":null,"links":[],"addresses":null,"maintenance":false,"errors":null}
        """)
        XCTAssertFalse(node.maintenance)
        XCTAssertNil(node.system)
        XCTAssertTrue(node.disks.isEmpty)
        XCTAssertTrue(node.errors.isEmpty)
    }

    func testTypedAddressIsTrimmed() {
        XCTAssertEqual(joinAddress("  192.168.1.42 \n"), "192.168.1.42")
        XCTAssertEqual(joinAddress("   "), "")
    }
}

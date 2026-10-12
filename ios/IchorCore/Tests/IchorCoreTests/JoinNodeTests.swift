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

    func testConfigPreviewDecodes() throws {
        let preview = try TalosJSON.decode(JoinConfigPreview.self, from: """
        {"redactedYaml":"machine:\\n    token: ******\\n","warnings":null,"needsBootstrap":false,"size":4096,
         "role":"worker","hostname":"worker-3","template":"192.0.2.13"}
        """)
        XCTAssertEqual(preview.hostname, "worker-3")
        XCTAssertEqual(preview.size, 4096)
        XCTAssertTrue(preview.warnings.isEmpty)
    }

    func testTemplatesAreReachableMembersWorkersFirst() {
        let nodes = [
            NodeOverview(node: "192.0.2.11", hostname: "cp-1", reachable: true, role: "controlplane"),
            NodeOverview(node: "192.0.2.12", hostname: "worker-2", reachable: true, role: "worker"),
            NodeOverview(node: "192.0.2.13", hostname: "worker-1", reachable: true, role: "worker"),
            NodeOverview(node: "192.0.2.14", hostname: "worker-9", reachable: false, role: "worker"),
            NodeOverview(node: "192.0.2.15", hostname: "odd", reachable: true, role: "unknown"),
            NodeOverview(node: "192.0.2.16", hostname: "cp-0", reachable: true, role: "init"),
        ]
        XCTAssertEqual(joinTemplates(nodes).map(\.hostname), ["worker-1", "worker-2", "cp-0", "cp-1"])
    }

    func testSuggestedHostnameIsTheFirstFree() {
        XCTAssertEqual(suggestedHostname(role: "worker", hostnames: []), "worker-1")
        XCTAssertEqual(suggestedHostname(role: "worker", hostnames: ["worker-1", "worker-2", "worker-4"]), "worker-3")
        XCTAssertEqual(suggestedHostname(role: "init", hostnames: ["controlplane-1"]), "controlplane-2")
    }

    func testInstallDisksAreWritableOnes() {
        let disks = [
            DiskInfo(name: "sda", devPath: "/dev/sda"),
            DiskInfo(name: "sr0", devPath: "/dev/sr0", readonly: true),
            DiskInfo(name: "loop0"),
        ]
        XCTAssertEqual(installDisks(disks), ["/dev/sda"])
    }

    func testTheDiskMustBeOneOfTheInspectedNode() throws {
        let node = try TalosJSON.decode(MaintenanceInspection.self, from: #"{"address":"10.0.0.5","maintenance":true,"disks":[{"name":"sda","devPath":"/dev/sda","size":1}]}"#)
        XCTAssertTrue(joinDiskFits("/dev/sda", node: node))
        XCTAssertFalse(joinDiskFits("/dev/nvme0n1", node: node))
        let bare = try TalosJSON.decode(MaintenanceInspection.self, from: #"{"address":"10.0.0.5","maintenance":true,"disks":[]}"#)
        XCTAssertTrue(joinDiskFits("/dev/vda", node: bare))
    }

    func testJoinProgressDecodes() throws {
        let event = try TalosJSON.decode(JoinProgress.self, from: """
        {"phase":"rebooting","message":"installing to /dev/sda and rebooting","at":1700000000000,"address":"192.0.2.20"}
        """)
        XCTAssertEqual(JoinPhase(rawValue: event.phase), .rebooting)
        XCTAssertEqual(event.address, "192.0.2.20")
        let bare = try TalosJSON.decode(JoinProgress.self, from: #"{"phase":"applying","message":"","at":1}"#)
        XCTAssertEqual(bare.address, "")
    }

    func testJoinTimelineMarksTheLatestPhaseCurrent() {
        let steps = joinTimeline([
            JoinProgress(phase: "applying", at: 1), JoinProgress(phase: "installing", at: 2),
            JoinProgress(phase: "rebooting", message: "down", at: 3), JoinProgress(phase: "rebooting", message: "still down", at: 4),
        ])
        XCTAssertEqual(steps.map(\.phase), JoinPhase.allCases)
        XCTAssertEqual(steps.map(\.state), [.done, .done, .current, .pending, .pending, .pending, .pending])
        XCTAssertEqual(steps[2].at, 3)
        XCTAssertEqual(steps[2].message, "still down")
    }

    func testJoinTimelineFailureAndSuccess() {
        let failed = joinTimeline([JoinProgress(phase: "applying"), JoinProgress(phase: "installing")], failure: "timeout")
        XCTAssertEqual(failed[1].state, .failed)
        XCTAssertEqual(failed[1].message, "timeout")
        XCTAssertEqual(failed[3].state, .pending)

        let refused = joinTimeline([], failure: "demo")
        XCTAssertEqual(refused.first?.state, .failed)

        let done = joinTimeline([JoinProgress(phase: "applying"), JoinProgress(phase: "done")], finished: true)
        XCTAssertTrue(done.allSatisfy { $0.state == .done })
    }
}

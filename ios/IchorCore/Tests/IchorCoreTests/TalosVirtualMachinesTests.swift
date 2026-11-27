import XCTest
@testable import IchorCore

final class TalosVirtualMachinesTests: XCTestCase {
    // A ClusterVirtualMachines answer (go/ichorgo/talos_vms.go), invented: one node with VMs,
    // one 1.15 node without, one older node, one that did not answer; a field from the future.
    private let json = #"""
        {"context":"lab","nodes":[
          {"node":"192.0.2.12","hostname":"hv-2","supported":true,"reason":"","vms":[],"libraries":[],"errors":{}},
          {"node":"192.0.2.11","hostname":"hv-1","supported":true,"reason":"","errors":{"libraries":"permission denied"},
           "libraries":[{"name":"images","path":"/var/mnt/images","phase":"ready","error":""}],"vms":[
            {"name":"ci-runner","powerState":"running","wantedPowerState":"running","stage":"ready","error":"",
             "running":true,"cpus":4,"memoryBytes":8589934592,"profile":"","addresses":["198.51.100.7/24"],
             "interfaces":[{"name":"eth0","mac":"52:54:00:aa:bb:01","addresses":["198.51.100.7/24"]}],
             "disks":[{"name":"root","pool":"pool-a","volume":"ci-runner-root","format":"qcow2",
               "sizeBytes":42949672960,"readOnly":false,"phase":"ready","error":"","future":true}]},
            {"name":"old-app","powerState":"stopped","wantedPowerState":"stopped","stage":"ready","running":false},
            {"name":"desk","powerState":"stopped","wantedPowerState":"running","stage":"error","error":"no image",
             "disks":[{"name":"root","phase":"notReady","error":"no image"}]},
            {"name":"booting","powerState":"stopped","wantedPowerState":"running","stage":"pending"}]},
          {"node":"192.0.2.13","hostname":"old-1","supported":false,
           "reason":"needs Talos v1.15 or newer (this node runs v1.14.2)","vms":[],"libraries":[],"errors":{}},
          {"node":"192.0.2.14","hostname":"192.0.2.14","supported":false,"vms":[],"libraries":[],"errors":{},
           "error":"node 192.0.2.14: connection refused"}]}
        """#

    private func cluster() throws -> TalosClusterVMs { try TalosJSON.decode(TalosClusterVMs.self, from: json) }

    func testDecodesTheReport() throws {
        let c = try cluster()
        XCTAssertEqual(c.context, "lab")
        XCTAssertEqual(c.nodes.count, 4)
        let hv1 = try XCTUnwrap(c.nodes.first { $0.hostname == "hv-1" })
        XCTAssertTrue(hv1.report.supported)
        XCTAssertEqual(hv1.report.errors["libraries"], "permission denied")
        XCTAssertEqual(hv1.report.libraries.first?.path, "/var/mnt/images")
        let runner = try XCTUnwrap(hv1.report.vms.first)
        XCTAssertEqual(runner.cpus, 4)
        XCTAssertEqual(runner.memoryBytes, 8 << 30)
        XCTAssertEqual(runner.firstAddress, "198.51.100.7")
        XCTAssertEqual(runner.interfaces.first?.mac, "52:54:00:aa:bb:01")
        XCTAssertEqual(runner.disks.first?.pool, "pool-a")
        XCTAssertEqual(runner.disks.first?.sizeBytes, 40 << 30)
        XCTAssertEqual(c.nodes.last?.error, "node 192.0.2.14: connection refused")
    }

    func testStates() throws {
        let vms = try cluster().all
        let states = Dictionary(uniqueKeysWithValues: vms.map { ($0.name, $0.vmState) })
        XCTAssertEqual(states, ["ci-runner": .running, "old-app": .stopped, "desk": .failing, "booting": .changing])
        XCTAssertEqual(TalosVMCounts(vms), TalosVMCounts(running: 1, stopped: 1, failing: 1, changing: 1))
        XCTAssertTrue(vms.first { $0.name == "desk" }?.disks.first?.bad == true)
        XCTAssertTrue(try cluster().busy)
    }

    func testShownNodesAndTheEntryRule() throws {
        let c = try cluster()
        // The empty 1.15 node is left out; the older and the failed nodes say why.
        XCTAssertEqual(c.shown.map(\.title), ["192.0.2.14", "hv-1", "old-1"])
        XCTAssertTrue(c.hasVMs)
        let empty = TalosClusterVMs.ofNode("192.0.2.12", hostname: "hv-2", report: TalosNodeVMs(supported: true))
        XCTAssertFalse(empty.hasVMs)
        XCTAssertTrue(empty.shown.isEmpty)
        let old = TalosNodeVMs(supported: false, reason: "needs Talos v1.15 or newer")
        XCTAssertFalse(TalosClusterVMs.ofNode("192.0.2.13", hostname: "old-1", report: old).hasVMs)
    }

    func testMissingFields() throws {
        let node = try TalosJSON.decode(TalosNodeVMs.self, from: #"{"vms":[{"name":"x"}]}"#)
        XCTAssertFalse(node.supported)
        XCTAssertFalse(node.hasVMs)
        XCTAssertEqual(node.vms.first?.stateLabel, "-")
        XCTAssertEqual(node.vms.first?.vmState, .stopped)
        XCTAssertNil(node.vms.first?.firstAddress)
    }
}

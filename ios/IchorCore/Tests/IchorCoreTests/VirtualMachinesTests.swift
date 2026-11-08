import XCTest
@testable import IchorCore

final class VirtualMachinesTests: XCTestCase {
    // A KubeVirtualMachines answer (go/ichorgo/kube_virt.go), cut down, with a field from the future.
    private let json = #"""
        {"installed":true,"harvester":true,"version":"v1","vms":[
          {"namespace":"vms","name":"build-runner","runStrategy":"Always","status":"CrashLoopBackOff","state":"failing",
           "ready":false,"created":false,"pendingChange":"","conditions":[
             {"type":"Failure","status":"True","reason":"CrashLoopBackOff","message":"virt-launcher exited"}],
           "cpu":8,"memory":"32Gi","node":"","phase":"","ips":[],"guestOS":"","launcherImage":"","migration":null,"disks":[]},
          {"namespace":"vms","name":"db-01","runStrategy":"Always","status":"Migrating","state":"changing","ready":true,"created":true,
           "conditions":[{"type":"Ready","status":"True"}],"cpu":4,"memory":"16Gi","node":"worker-2","phase":"Running",
           "ips":["10.244.2.41","fd00::41"],"guestOS":"Rocky Linux 10.0",
           "migration":{"name":"db-01-migration-x7k2p","phase":"Running","sourceNode":"worker-2","targetNode":"worker-3",
             "startedAt":1760000000000,"completed":false,"failed":false},"disks":[]},
          {"namespace":"lab","name":"win11-test","runStrategy":"Always","status":"Provisioning","state":"changing",
           "disks":[{"name":"win11-test-root","phase":"ImportInProgress","progress":"62.00%","importing":true}],"future":1},
          {"namespace":"vms","name":"web-01","runStrategy":"Always","status":"Running","state":"running","ready":true,
           "node":"worker-1","ips":["10.244.1.23"],"guestOS":"Debian GNU/Linux 13 (trixie)"},
          {"namespace":"lab","name":"k3s-sandbox","runStrategy":"Halted","status":"Stopped","state":"stopped"},
          {"namespace":"lab","name":"paused-one","runStrategy":"Always","status":"Paused","state":"changing"}],
         "namespaces":["lab","vms"],
         "counts":{"running":1,"stopped":1,"failing":1,"migrating":1,"importing":1}}
        """#

    private func report() throws -> VirtualMachines { try TalosJSON.decode(VirtualMachines.self, from: json) }
    private func vm(_ name: String) throws -> VirtualMachine { try XCTUnwrap(report().vms.first { $0.name == name }) }
    private func allowed(_ vm: VirtualMachine) -> Set<VmAction> { Set(VmAction.allCases.filter(vm.allows)) }

    func testDecodesTheReport() throws {
        let r = try report()
        XCTAssertTrue(r.installed)
        XCTAssertTrue(r.harvester)
        XCTAssertEqual(r.vms.count, 6)
        XCTAssertEqual(r.namespaces, ["lab", "vms"])
        XCTAssertEqual(r.counts, VmCounts(running: 1, stopped: 1, failing: 1, migrating: 1, importing: 1))
        let db = try vm("db-01")
        XCTAssertEqual(db.id, "vms/db-01")
        XCTAssertEqual(db.firstIP, "10.244.2.41")
        XCTAssertEqual(db.migration?.targetNode, "worker-3")
        XCTAssertEqual(try vm("build-runner").vmState, .failing)
        XCTAssertTrue(try vm("build-runner").conditions[0].bad)
        XCTAssertEqual(try vm("web-01").vmState, .running)
        XCTAssertEqual(try vm("k3s-sandbox").vmState, .stopped)
    }

    func testNotInstalledDecodesWithDefaults() throws {
        let empty = try TalosJSON.decode(VirtualMachines.self, from: #"{"installed":false}"#)
        XCTAssertFalse(empty.installed)
        XCTAssertTrue(empty.vms.isEmpty)
        XCTAssertEqual(empty.counts, VmCounts())
    }

    func testProgressShowsAnImportOrAMigrationInFlight() throws {
        XCTAssertEqual(try vm("win11-test").progress, .importing(disk: "win11-test-root", progress: "62.00%"))
        XCTAssertEqual(try vm("db-01").progress, .migrating(from: "worker-2", to: "worker-3"))
        XCTAssertNil(try vm("web-01").progress)
    }

    func testActionsAreAllowedAsTheGoCoreRefusesThem() throws {
        XCTAssertEqual(allowed(try vm("web-01")), [.stop, .restart, .migrate, .pause])
        // runStrategy Halted: a manual restart is refused, start is the way back.
        XCTAssertEqual(allowed(try vm("k3s-sandbox")), [.start])
        // Its instance runs: start is pointless (stricter than the Go core).
        XCTAssertEqual(allowed(try vm("db-01")), [.stop, .pause])
        XCTAssertEqual(allowed(try vm("win11-test")), [.stop])
        XCTAssertEqual(allowed(try vm("paused-one")), [.stop, .unpause])
    }

    func testActionsMapToTheirAccessChecks() {
        XCTAssertEqual(VmAction.stop.kubeAction, .vmStop)
        XCTAssertEqual(VmAction.unpause.kubeAction, .vmUnpause)
        XCTAssertEqual(VmAction.unpause.rawValue, "unpause")
    }

    func testFiltersByNamespaceAndPollsWhileBusy() throws {
        let r = try report()
        XCTAssertEqual(r.inNamespace("lab").map(\.name), ["win11-test", "k3s-sandbox", "paused-one"])
        XCTAssertEqual(r.inNamespace(nil).count, 6)
        XCTAssertTrue(r.busy)
    }

    func testActionResultDecodes() throws {
        let r = try TalosJSON.decode(
            VmActionResult.self,
            from: #"{"action":"migrate","namespace":"vms","name":"web-01","migration":"web-01-migration-abcde"}"#)
        XCTAssertEqual(r.migration, "web-01-migration-abcde")
    }
}

import XCTest

@testable import IchorCore

final class OmniAllocationTests: XCTestCase {
    func testMachineSetsDecode() throws {
        let sets = try TalosJSON.decode(
            OmniMachineSets.self,
            from: """
                {"cluster":"lab","machineSets":[
                 {"id":"lab-control-planes","role":"controlplane","allocation":{"className":"","type":"none","count":0},"matchLabels":null},
                 {"id":"lab-workers","role":"worker","allocation":{"className":"small","type":"static","count":2},
                  "matchLabels":["omni.sidero.dev/arch = amd64"]}]}
                """)
        XCTAssertEqual(sets.cluster, "lab")
        XCTAssertTrue(sets.machineSets[0].isControlPlane)
        XCTAssertEqual(sets.machineSets[0].matchLabels, [])
        XCTAssertEqual(sets.machineSets[1].allocation.className, "small")
        XCTAssertEqual(sets.machineSets[1].allocation.count, 2)
    }

    func testMachinesDecodeAndName() throws {
        let machines = try TalosJSON.decode(
            [OmniMachine].self,
            from: """
                [{"id":"aaaaaaaa-1111","hostname":"talos-new","addresses":["192.0.2.7"],"talosVersion":"v1.11.2",
                  "connected":true,"maintenance":true,"labels":{"ichor/role":"worker"}},
                 {"id":"bbbbbbbb-2222","hostname":"","addresses":null,"talosVersion":"","connected":false,"maintenance":false,"labels":null}]
                """)
        XCTAssertEqual(machines[0].name, "talos-new")
        XCTAssertTrue(machines[0].maintenance)
        XCTAssertEqual(machines[0].labels["ichor/role"], "worker")
        XCTAssertEqual(machines[1].name, "bbbbbbbb-2222")
        XCTAssertEqual(machines[1].addresses, [])
    }

    func testLabelsCarryTheRoleAndWhatWasTyped() {
        XCTAssertEqual(omniLabelsJSON(role: "worker", extra: ""), #"{"ichor/role":"worker"}"#)
        XCTAssertEqual(
            omniLabelsJSON(role: "controlplane", extra: " rack = b2 , spare,, "),
            #"{"ichor/role":"controlplane","rack":"b2","spare":""}"#)
        XCTAssertEqual(omniLabelsJSON(role: "worker", extra: "ichor/role=mine"), #"{"ichor/role":"mine"}"#)
    }

    func testAnAllocationHasItsOwnTimeline() {
        let request = JoinRequest(
            address: "", template: "", role: "worker", hostname: "talos-new", disk: "", staticAddress: "",
            omni: true, omniAllocate: OmniAllocateSpec(machineID: "aaaaaaaa-1111", machineSetID: "lab-workers"))
        XCTAssertEqual(request.phases, JoinPhase.omniAllocateRun)
        XCTAssertEqual(request.withReplacing(false).omniAllocate, request.omniAllocate)
        let steps = joinTimeline([JoinProgress(phase: "booting")], phases: request.phases)
        XCTAssertEqual(steps.map(\.phase), [.allocated, .installing, .booting, .done])
    }
}

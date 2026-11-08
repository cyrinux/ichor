import XCTest

@testable import IchorCore

final class CloudHetznerTests: XCTestCase {
    private let sample = """
        {"currency":"EUR",
         "serverTypes":[{"name":"cx23","cores":2,"memoryGB":4,"diskGB":40,"arch":"amd64",
                         "prices":[{"location":"fsn1","hourly":"0.0060","monthly":"3.49"}]},
                        {"name":"cax11","cores":2,"memoryGB":4,"diskGB":40,"arch":"arm64","prices":null}],
         "locations":[{"name":"fsn1","description":"Falkenstein","city":"Falkenstein","country":"DE"}],
         "snapshots":[{"id":3,"description":"talos arm64","labels":{"os":"talos"},"created":"2026-10-01T00:00:00+00:00",
                       "arch":"arm64","osFlavor":"unknown","talos":true},
                      {"id":2,"description":"talos amd64","labels":null,"created":"2026-08-01T00:00:00+00:00",
                       "arch":"amd64","osFlavor":"unknown","talos":true}]}
        """

    func testOptionsDecodeAndFilterSnapshotsByArchitecture() throws {
        let options = try TalosJSON.decode(HetznerOptions.self, from: sample)
        XCTAssertEqual(options.currency, "EUR")
        XCTAssertEqual(options.serverTypes[0].price(in: "fsn1")?.monthly, "3.49")
        XCTAssertNil(options.serverTypes[0].price(in: "hel1"))
        XCTAssertEqual(options.serverTypes[1].prices, [])
        XCTAssertEqual(options.locations[0].city, "Falkenstein")
        XCTAssertEqual(options.snapshots(for: options.serverTypes[0]).map(\.id), [2])
        XCTAssertEqual(options.snapshots(for: nil), [])
    }

    func testEmptyOptionsAndTokenCheckDecode() throws {
        let options = try TalosJSON.decode(
            HetznerOptions.self, from: #"{"currency":"","serverTypes":null,"locations":null,"snapshots":null}"#)
        XCTAssertEqual(options.serverTypes, [])
        let check = try TalosJSON.decode(HetznerTokenCheck.self, from: #"{"readOnly":true,"servers":2}"#)
        XCTAssertTrue(check.readOnly)
        XCTAssertEqual(check.servers, 2)
    }

    func testCloudRunTimelinesKeepTheirOwnSteps() {
        let talos = joinTimeline(
            [JoinProgress(phase: "creating"), JoinProgress(phase: "starting")], phases: JoinPhase.cloudRun)
        XCTAssertEqual(talos.map(\.phase), JoinPhase.cloudRun)
        XCTAssertEqual(talos[0].state, .done)
        XCTAssertEqual(talos[1].state, .current)

        // The deletion after a failure has no step of its own: the failure stays on its phase.
        let failed = joinTimeline(
            [JoinProgress(phase: "creating"), JoinProgress(phase: "cleanup")], failure: "no capacity",
            phases: JoinPhase.cloudOmniRun)
        XCTAssertEqual(failed.map(\.phase), JoinPhase.cloudOmniRun)
        XCTAssertEqual(failed[0].state, .failed)
    }

    func testARequestKeepsItsServerWhenReplacingChanges() {
        let spec = HetznerCreateSpec(serverType: "cx23", location: "fsn1", snapshotId: 2, hostname: "node-c")
        let request = JoinRequest(
            address: "", template: "", role: "", hostname: "node-c", disk: "", staticAddress: "", hetzner: spec,
            omni: true)
        XCTAssertEqual(request.phases, JoinPhase.cloudOmniRun)
        XCTAssertEqual(request.withReplacing(true).hetzner, spec)
        XCTAssertTrue(request.withReplacing(true).omni)
    }

    func testASnapshotRunHasItsOwnSteps() throws {
        let spec = HetznerSnapshotSpec(arch: "amd64", location: "fsn1")
        let request = JoinRequest(
            address: "", template: "", role: "", hostname: "Talos amd64", disk: "", staticAddress: "", snapshot: spec)
        XCTAssertEqual(request.phases, JoinPhase.snapshotRun)
        XCTAssertEqual(request.withReplacing(true).snapshot, spec)

        let steps = joinTimeline(
            [JoinProgress(phase: "key"), JoinProgress(phase: "writing")], phases: JoinPhase.snapshotRun)
        XCTAssertEqual(steps.map(\.phase), JoinPhase.snapshotRun)
        XCTAssertEqual(steps[0].state, .done)
    }

    func testASnapshotResultAndItsCostDecode() throws {
        let result = try JSONDecoder().decode(
            HetznerSnapshotResult.self,
            from: Data(#"{"imageId":33,"description":"Talos v1.14.2","labels":{"os":"talos"},"sizeGB":1.5}"#.utf8))
        XCTAssertEqual(result.imageId, 33)
        XCTAssertEqual(result.sizeGB, 1.5)
        XCTAssertEqual(result.created, "")

        let cost = try JSONDecoder().decode(
            HetznerSnapshotCost.self,
            from: Data(#"{"serverType":"cx23","hourly":"0.0060","monthlyPerGB":"0.0119","currency":"EUR"}"#.utf8))
        XCTAssertEqual(cost.serverType, "cx23")
        XCTAssertEqual(cost.monthlyPerGB, "0.0119")
    }
}

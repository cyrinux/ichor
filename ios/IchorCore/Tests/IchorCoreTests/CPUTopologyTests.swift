import XCTest
@testable import IchorCore

final class CPUTopologyTests: XCTestCase {
    private let json = #"""
        {"numa":[{"id":0,"cpus":"0-7","cpuCount":8,"memoryBytes":34359738368},
        {"id":1,"cpus":"8-15","cpuCount":8,"memoryBytes":34359738368}],
        "cpuScaling":[{"policy":"policy0","cpus":"0-7","driver":"intel_pstate","governor":"performance",
        "energyPreference":"performance","minKhz":800000,"maxKhz":5100000,"coreType":"performance",
        "configuredBy":"performance-cores"},
        {"policy":"policy8","cpus":"8-15","driver":"intel_pstate","governor":"powersave",
        "energyPreference":"","minKhz":800000,"maxKhz":3000000,"coreType":"","configuredBy":""}],
        "errors":{}}
        """#

    func testDecodeGoJSON() throws {
        let hw = try TalosJSON.decode(NodeHardware.self, from: json)
        let expected = [
            NUMANode(id: 0, cpus: "0-7", cpuCount: 8, memoryBytes: 34_359_738_368),
            NUMANode(id: 1, cpus: "8-15", cpuCount: 8, memoryBytes: 34_359_738_368),
        ]
        XCTAssertEqual(hw.numa, expected)
        XCTAssertEqual(
            hw.cpuScaling.first,
            CPUScalingPolicy(
                policy: "policy0", cpus: "0-7", driver: "intel_pstate", governor: "performance",
                energyPreference: "performance", minKhz: 800_000, maxKhz: 5_100_000, coreType: "performance",
                configuredBy: "performance-cores"))
        XCTAssertTrue(hw.showsNUMA)
        XCTAssertTrue(hw.showsCPUScaling)
        XCTAssertTrue(hw.powersaveWhileThrottled(true))
        XCTAssertFalse(hw.powersaveWhileThrottled(false))
    }

    func testOlderCoreAndSingleNode() throws {
        // A core before this field, or a Talos 1.14 node (null lists): neither section shows.
        let old = try TalosJSON.decode(NodeHardware.self, from: #"{"numa":null,"errors":{}}"#)
        XCTAssertEqual(old.numa, [])
        XCTAssertEqual(old.cpuScaling, [])
        XCTAssertFalse(old.showsNUMA)
        XCTAssertFalse(old.showsCPUScaling)

        let single = NodeHardware(numa: [NUMANode(id: 0, cpus: "0-3", cpuCount: 4)])
        XCTAssertFalse(single.showsNUMA)

        let failed = NodeHardware(errors: ["cpuScaling": "unavailable"])
        XCTAssertTrue(failed.showsCPUScaling)
    }
}

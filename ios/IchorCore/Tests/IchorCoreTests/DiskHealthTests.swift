import XCTest
@testable import IchorCore

final class DiskHealthTests: XCTestCase {
    func testDecoding() throws {
        let report = try TalosJSON.decode(NodeDiskHealth.self, from: """
        {"supported":true,"reason":"","disks":[
          {"device":"nvme0n1","model":"Samsung 980","serial":"S1","healthy":true,"type":"nvme","message":"","powerState":"active",
           "temperatureC":41,"powerOnHours":12345,"powerCycles":80,"wearPercent":7,"criticalWarnings":[],
           "attributes":[{"k":"Available spare","v":"100 %"}]},
          {"device":"sda","healthy":null,"temperatureC":null,"powerOnHours":null,"wearPercent":null,
           "criticalWarnings":null,"attributes":null},
          {"device":"sdb","healthy":false,"message":"FAILED","criticalWarnings":["Reallocated sectors above threshold"]}]}
        """)
        XCTAssertTrue(report.supported)
        XCTAssertEqual(report.disks[0], DiskHealthInfo(device: "nvme0n1", model: "Samsung 980", serial: "S1", healthy: true,
                                                       temperatureC: 41, powerOnHours: 12345, wearPercent: 7,
                                                       attributes: [LogField(k: "Available spare", v: "100 %")]))
        XCTAssertEqual(report.disks[1], DiskHealthInfo(device: "sda"))
        XCTAssertEqual(report.disks[2].healthy, false)
        XCTAssertEqual(report.disks[2].message, "FAILED")
        XCTAssertEqual(report.disks[2].criticalWarnings.count, 1)

        let old = try TalosJSON.decode(NodeDiskHealth.self, from: #"{"supported":false,"reason":"needs Talos v1.15 or newer (this node runs v1.14.0)","disks":[]}"#)
        XCTAssertEqual(old, NodeDiskHealth(supported: false, reason: "needs Talos v1.15 or newer (this node runs v1.14.0)"))
        XCTAssertEqual(versionNotice(old.reason), VersionNotice(minVersion: "v1.15"))
    }

    func testState() {
        XCTAssertEqual(DiskHealthInfo(device: "a", healthy: true).state, .healthy)
        XCTAssertEqual(DiskHealthInfo(device: "a", healthy: false).state, .failing)
        XCTAssertEqual(DiskHealthInfo(device: "a").state, .unknown)
        // Critical warnings win over a passing verdict.
        XCTAssertEqual(DiskHealthInfo(device: "a", healthy: true, criticalWarnings: ["spare below threshold"]).state, .failing)
        XCTAssertEqual(DiskHealthInfo(device: "a", criticalWarnings: ["x"]).state, .failing)
    }

    func testSorting() {
        let disks = [DiskHealthInfo(device: "/dev/sdb", healthy: true), DiskHealthInfo(device: "/dev/sda", healthy: true),
                     DiskHealthInfo(device: "/dev/sdd"), DiskHealthInfo(device: "/dev/sdc", healthy: false)]
        XCTAssertEqual(sortDiskHealth(disks).map(\.device), ["/dev/sdc", "/dev/sdd", "/dev/sda", "/dev/sdb"])
    }

    func testWearAndTemperature() {
        XCTAssertEqual(wearLevel(nil), .normal)
        XCTAssertEqual(wearLevel(79), .normal)
        XCTAssertEqual(wearLevel(80), .warning)
        XCTAssertEqual(wearLevel(95), .critical)
        XCTAssertEqual(formatTemperature(41), "41 °C")
        XCTAssertEqual(formatTemperature(41.5), "41.5 °C")
    }
}

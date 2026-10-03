import XCTest
@testable import IchorCore

final class CgroupsTests: XCTestCase {
    private func report(at: Int64, etcdCPU: UInt64, kubeletCPU: UInt64, etcdIO: UInt64 = 0) -> CgroupReport {
        CgroupReport(at: at, root: CgroupNode(name: ".", children: [
            CgroupNode(name: "podruntime", memCurrent: 300, children: [
                CgroupNode(name: "etcd", kind: "service", memCurrent: 200, cpuUsec: etcdCPU, ioWrite: etcdIO,
                           pressure: CgroupPressure(io: CgroupPSI(some10: 30))),
                CgroupNode(name: "kubelet", kind: "service", memCurrent: 100, cpuUsec: kubeletCPU),
            ]),
            CgroupNode(name: "init", kind: "service", memCurrent: 50),
        ]))
    }

    func testDecodesGoJSONWithOmittedFields() throws {
        let json = """
        {"at":1,"pressure":{"cpu":{"some10":1.5,"some60":1,"full10":0,"full60":0},"memory":{"some10":0,"some60":0,"full10":0,"full60":0},"io":{"some10":0,"some60":0,"full10":0,"full60":0}},
         "hotspots":[{"resource":"io","name":"etcd","parent":"podruntime","some10":7}],
         "alerts":[{"kind":"oomKill","name":"apid","parent":"system","count":2},{"kind":"memoryLimit","name":"kubepods","parent":".","percent":95}],
         "root":{"name":".","kind":"group","children":[{"name":"init","kind":"service","memCurrent":10}]}}
        """
        let r = try TalosJSON.decode(CgroupReport.self, from: json)
        XCTAssertEqual(r.pressure.cpu.some10, 1.5)
        XCTAssertEqual(r.mostAffected("io"), "etcd (podruntime)")
        XCTAssertNil(r.mostAffected("cpu"))
        XCTAssertEqual(r.alerts.first?.who, "apid (system)")
        XCTAssertEqual(r.alerts.last?.percent, 95)
        XCTAssertEqual(r.root?.children.first?.memCurrent, 10)
        XCTAssertEqual(r.root?.children.first?.children, [])
    }

    func testCollapsedTreeShowsTopGroupsOnly() {
        let rows = cgroupRows(previous: nil, current: report(at: 1000, etcdCPU: 0, kubeletCPU: 0), expanded: [], sort: .memory)
        XCTAssertEqual(rows.map(\.node.name), ["podruntime", "init"])
        XCTAssertNil(rows[0].cpuPercent)
    }

    func testCPUAndIOAreRatesBetweenSamples() {
        let previous = report(at: 10000, etcdCPU: 1_000_000, kubeletCPU: 0, etcdIO: 1000)
        let current = report(at: 12000, etcdCPU: 2_000_000, kubeletCPU: 4_000_000, etcdIO: 5000)
        let rows = cgroupRows(previous: previous, current: current, expanded: ["podruntime"], sort: .cpu)
        XCTAssertEqual(rows.map(\.node.name), ["podruntime", "kubelet", "etcd", "init"])
        let etcd = rows.first { $0.node.name == "etcd" }
        XCTAssertEqual(etcd?.cpuPercent ?? -1, 50, accuracy: 1e-9)
        XCTAssertEqual(etcd?.ioPerSecond ?? -1, 2000, accuracy: 1e-9)
        XCTAssertEqual(etcd?.depth, 1)
        XCTAssertEqual(rows.first { $0.node.name == "kubelet" }?.cpuPercent ?? -1, 200, accuracy: 1e-9)
    }

    func testCounterResetHasNoRate() {
        let rows = cgroupRows(previous: report(at: 1000, etcdCPU: 9_000_000, kubeletCPU: 0),
                              current: report(at: 2000, etcdCPU: 1, kubeletCPU: 0), expanded: ["podruntime"], sort: .memory)
        XCTAssertNil(rows.first { $0.node.name == "etcd" }?.cpuPercent)
    }

    func testPressureSortAndLevels() {
        let rows = cgroupRows(previous: nil, current: report(at: 1, etcdCPU: 0, kubeletCPU: 0), expanded: ["podruntime"], sort: .pressure)
        XCTAssertEqual(rows[1].node.name, "etcd")
        XCTAssertEqual(pressureLevel(4.99), .ok)
        XCTAssertEqual(pressureLevel(5), .warn)
        XCTAssertEqual(pressureLevel(20), .bad)
    }

    func testDefaultExpansionKeepsKubepodsClosed() {
        let r = CgroupReport(at: 0, root: CgroupNode(name: ".", children: [
            CgroupNode(name: "kubepods", children: [
                CgroupNode(name: "burstable", children: [CgroupNode(name: "ns/p", kind: "pod")]),
                CgroupNode(name: "ns/g", kind: "pod"),
            ]),
            CgroupNode(name: "init", kind: "service"),
        ]))
        let rows = cgroupRows(previous: nil, current: r, expanded: defaultExpandedCgroups(r), sort: .memory)
        // kubepods stays closed: its pods are the Pods tab's.
        XCTAssertEqual(rows.map(\.node.name), ["kubepods", "init"])
    }

    func testOnlyAdminsReadCgroups() {
        XCTAssertTrue(ContextSummary(name: "x", roles: ["os:admin"]).allows(.cgroups))
        XCTAssertFalse(ContextSummary(name: "x", roles: ["os:operator"]).allows(.cgroups))
        XCTAssertFalse(ContextSummary(name: "x", roles: ["os:reader"]).allows(.cgroups))
    }
}

import XCTest
@testable import IchorCore

final class ProcessesTests: XCTestCase {
    func testDecodeGoJSON() throws {
        let json = #"{"at":1800000000000,"processes":[{"pid":1,"ppid":0,"state":"S","threads":12,"cpuTime":3.5,"rss":10485760,"vms":20971520,"command":"machined","args":"/sbin/init"}]}"#
        let sample = try TalosJSON.decode(ProcessSample.self, from: json)
        XCTAssertEqual(sample.at, 1_800_000_000_000)
        XCTAssertEqual(sample.processes.first?.command, "machined")
        XCTAssertEqual(sample.processes.first?.rss, 10_485_760)
        XCTAssertEqual(sample.processes.first?.cpuTime ?? 0, 3.5, accuracy: 0.0001)
    }

    func testNullProcessesDecodeAsEmpty() throws {
        let sample = try TalosJSON.decode(ProcessSample.self, from: #"{"at":5,"processes":null}"#)
        XCTAssertEqual(sample.processes, [])
    }

    func testCPUPercentFromDeltas() {
        let before = ProcessSample(at: 0, processes: [
            NodeProcess(pid: 1, cpuTime: 10, command: "a"),
            NodeProcess(pid: 2, cpuTime: 5, command: "b"),
            NodeProcess(pid: 3, cpuTime: 100, command: "old"),
        ])
        let after = ProcessSample(at: 2_000, processes: [
            NodeProcess(pid: 1, cpuTime: 11, command: "a"), // 1 s over 2 s
            NodeProcess(pid: 2, cpuTime: 9, command: "b"), // 4 s over 2 s: two cores
            NodeProcess(pid: 3, cpuTime: 0.5, command: "new"), // pid reused
            NodeProcess(pid: 4, cpuTime: 1, command: "c"), // new pid
        ])
        let rows = processRows(previous: before, current: after)
        XCTAssertEqual(rows[0].cpuPercent ?? -1, 50, accuracy: 0.001)
        XCTAssertEqual(rows[1].cpuPercent ?? -1, 200, accuracy: 0.001)
        XCTAssertNil(rows[2].cpuPercent)
        XCTAssertNil(rows[3].cpuPercent)
    }

    func testNoPreviousOrNoElapsedTime() {
        let sample = ProcessSample(at: 10, processes: [NodeProcess(pid: 1, cpuTime: 1, command: "a")])
        XCTAssertNil(processRows(previous: nil, current: sample)[0].cpuPercent)
        XCTAssertNil(processRows(previous: sample, current: sample)[0].cpuPercent)
    }

    func testCounterGoingBackwardsGivesZero() {
        let before = ProcessSample(at: 0, processes: [NodeProcess(pid: 1, cpuTime: 10, command: "a")])
        let after = ProcessSample(at: 1_000, processes: [NodeProcess(pid: 1, cpuTime: 2, command: "a")])
        XCTAssertEqual(processRows(previous: before, current: after)[0].cpuPercent, 0)
    }

    func testSortByCPUThenMemory() {
        let rows = [
            ProcessRow(process: NodeProcess(pid: 1, rss: 10, command: "a"), cpuPercent: nil),
            ProcessRow(process: NodeProcess(pid: 2, rss: 5, command: "b"), cpuPercent: 30),
            ProcessRow(process: NodeProcess(pid: 3, rss: 50, command: "c"), cpuPercent: 30),
            ProcessRow(process: NodeProcess(pid: 4, rss: 1, command: "d"), cpuPercent: 90),
        ]
        XCTAssertEqual(sortProcesses(rows, by: .cpu).map(\.id), [4, 3, 2, 1])
        XCTAssertEqual(sortProcesses(rows, by: .memory).map(\.id), [3, 1, 2, 4])
    }

    func testFilterOnCommandAndArgs() {
        let rows = [
            ProcessRow(process: NodeProcess(pid: 1, command: "kubelet", args: "/usr/local/bin/kubelet --v=2"), cpuPercent: nil),
            ProcessRow(process: NodeProcess(pid: 2, command: "etcd", args: "/usr/local/bin/etcd --name cp-1"), cpuPercent: nil),
        ]
        XCTAssertEqual(filterProcesses(rows, query: "KUBE").map(\.id), [1])
        XCTAssertEqual(filterProcesses(rows, query: "cp-1").map(\.id), [2])
        XCTAssertEqual(filterProcesses(rows, query: "  ").map(\.id), [1, 2])
    }

    func testTotalRSS() {
        XCTAssertEqual(totalRSS([NodeProcess(pid: 1, rss: 100, command: "a"), NodeProcess(pid: 2, rss: 23, command: "b")]), 123)
        XCTAssertEqual(totalRSS([]), 0)
    }
}

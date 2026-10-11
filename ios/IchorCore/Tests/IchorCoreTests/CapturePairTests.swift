import XCTest
@testable import IchorCore

final class CapturePairTests: XCTestCase {
    func testThePairFileNamesBothNodes() {
        let name = pairCaptureFilename(hostname: "worker-1", peerHostname: "worker 2", interface: "eth0",
                                       date: Date(timeIntervalSince1970: 0), timeZone: TimeZone(identifier: "UTC")!)
        XCTAssertEqual(name, "worker-1_worker-2-eth0-19700101-000000.pcap")
    }

    func testTheInterfacesOfAPair() {
        var options = CaptureOptions(interface: "eth0")
        XCTAssertFalse(options.isPaired)
        XCTAssertEqual(options.interfaces, "eth0")
        options.peer = "192.0.2.12"
        XCTAssertEqual(options.interfaces, "eth0")
        options.peerInterface = "wg0"
        XCTAssertEqual(options.interfaces, "eth0,wg0")
        options.peerInterface = "eth0"
        XCTAssertEqual(options.interfaces, "eth0")
    }

    func testPairStatsAndNodeSummariesDecode() throws {
        let stats = try TalosJSON.decode(PairCaptureStats.self, from: """
        {"packets":7,"bytes":900,"nodes":[{"node":"192.0.2.11","hostname":"worker-1","packets":3,"bytes":400},
                                         {"node":"192.0.2.12","hostname":"worker-2","packets":4,"bytes":500}]}
        """)
        XCTAssertEqual(stats.nodes.map(\.hostname), ["worker-1", "worker-2"])
        let summary = try TalosJSON.decode(PacketSummary.self, from: #"{"n":0,"proto":"UDP","node":"worker-2"}"#)
        XCTAssertEqual(summary.node, "worker-2")
        XCTAssertEqual(try TalosJSON.decode(PacketSummary.self, from: #"{"n":1}"#).node, "")
    }

    func testTheClockGapDecodes() throws {
        XCTAssertEqual(try TalosJSON.decode(PcapPage.self, from: #"{"packets":[],"total":0,"clockGapMs":12}"#).clockGapMs, 12)
        XCTAssertNil(try TalosJSON.decode(PcapPage.self, from: #"{"packets":[],"total":0}"#).clockGapMs)
        XCTAssertEqual(try TalosJSON.decode(PairCaptureStats.self, from: #"{"packets":1,"bytes":2,"nodes":[],"clockGapMs":3}"#).clockGapMs, 3)
    }
}

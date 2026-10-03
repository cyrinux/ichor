import XCTest
@testable import IchorCore

final class NetPerfTests: XCTestCase {
    private func node(_ name: String, controlPlane: Bool = false, ready: Bool = true) -> NetPerfNode {
        NetPerfNode(name: name, controlPlane: controlPlane, ready: ready)
    }

    private func pair(_ server: String, _ client: String) -> NetPerfPair { NetPerfPair(server: server, client: client) }

    func testDecodesTheGoJSON() throws {
        let json = #"{"server":"a","client":"b","hostNetwork":true,"seconds":10,"image":"img","started":1,"finished":2,"results":["# +
            #"{"path":"pod","test":"throughput","throughputMbps":8734.5},"# +
            #"{"path":"pod","test":"latency","transactionRate":15873.2,"latencyUs":{"min":38,"mean":62.4,"max":1874,"p50":58,"p90":74,"p99":131}},"# +
            #"{"path":"host","test":"throughput","error":"no answer from netserver"}]}"#
        let report = try TalosJSON.decode(NetPerfReport.self, from: json)
        XCTAssertEqual(report.results[0].throughputMbps, 8734.5)
        XCTAssertEqual(try XCTUnwrap(report.results[1].latency).p99, 131)
        XCTAssertEqual(report.results[2].error, "no answer from netserver")
        XCTAssertEqual(try TalosJSON.decode(NetPerfReport.self, from: #"{"results":null}"#), NetPerfReport())

        let progress = try TalosJSON.decode(
            NetPerfProgress.self,
            from: #"{"phase":"testing","path":"pod","test":"latency","step":2,"steps":4,"at":5,"results":[]}"#
        )
        XCTAssertEqual(progress.phase, NetPerfPhase.testing)
        XCTAssertEqual(progress.step, 2)
    }

    func testDefaultPairPrefersReadyWorkers() {
        let nodes = [node("cp-1", controlPlane: true), node("w-1", ready: false), node("w-2"), node("w-3")]
        XCTAssertEqual(defaultNetPerfPair(nodes), pair("w-2", "w-3"))
        XCTAssertEqual(defaultNetPerfPair([node("cp-1", controlPlane: true), node("w-2")]), pair("cp-1", "w-2"))
        XCTAssertEqual(defaultNetPerfPair([node("solo", controlPlane: true)]), pair("solo", "solo"))
        XCTAssertNil(defaultNetPerfPair([node("down", ready: false)]))
    }

    func testSetupKeepsChosenNodesThatAreStillReady() {
        let nodes = [node("a"), node("b"), node("c"), node("gone", ready: false)]
        XCTAssertEqual(NetPerfSetup(server: "c", client: "a").withNodes(nodes), NetPerfSetup(server: "c", client: "a"))
        XCTAssertEqual(NetPerfSetup(server: "gone", client: "x").withNodes(nodes), NetPerfSetup(server: "a", client: "b"))
        XCTAssertFalse(NetPerfSetup().withNodes([]).ready)
        XCTAssertTrue(NetPerfSetup().withNodes(nodes).ready)
        XCTAssertEqual(NetPerfSetup(hostNetwork: true).steps, 4)
        XCTAssertEqual(NetPerfSetup(hostNetwork: true).paths, [NetPerfPath.pod, NetPerfPath.host])
    }

    func testFormatsRatesAndLatencies() {
        XCTAssertEqual(formatMbps(9412.8), "9.41 Gbit/s")
        XCTAssertEqual(formatMbps(338.15), "338 Mbit/s")
        XCTAssertEqual(formatMbps(4.2), "4.2 Mbit/s")
        XCTAssertEqual(formatMicros(58), "58 µs")
        XCTAssertEqual(formatMicros(3542), "3.54 ms")
    }
}

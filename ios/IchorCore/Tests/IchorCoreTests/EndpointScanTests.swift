import XCTest
@testable import IchorCore

final class EndpointScanTests: XCTestCase {
    func testOwnNetworkFirstThenListedThenCommon() {
        let networks = scanNetworks(
            local: [LocalAddress("192.168.42.17", 24)],
            known: ["10.20.30.40", "10.20.30.41:50001", "203.0.113.5", "talos.example.com", "fd00::1"]
        )
        XCTAssertEqual(networks, ["192.168.42.0/24", "10.20.30.0/24", "fd00::/120"] + commonPrivateNetworks)
    }

    func testOwnUlaNetworkIsSweptFromTheStartOfItsPrefix() {
        // SLAAC addresses are random: statically numbered nodes sit low in the /64.
        let networks = scanNetworks(
            local: [
                LocalAddress("192.168.42.17", 24),
                LocalAddress("fd12:3456:789a:1:abcd:ef01:2345:6789", 64),
                LocalAddress("fd12:3456:789a:2::1:5", 120),
            ],
            known: []
        )
        XCTAssertEqual(Array(networks.prefix(3)), ["192.168.42.0/24", "fd12:3456:789a:1::/120", "fd12:3456:789a:2::1:0/120"])
    }

    func testGlobalAndLinkLocalIpv6AreSkipped() {
        let networks = scanNetworks(
            local: [LocalAddress("2001:db8::5", 64), LocalAddress("fe80::1", 64)],
            known: ["2001:db8::7", "fe80::2%wlan0", "[2001:db8::8]:50000"]
        )
        XCTAssertEqual(networks, commonPrivateNetworks)
    }

    func testListedIpv6EndpointsGiveTheirNeighbourhood() {
        let networks = scanNetworks(
            local: [LocalAddress("fd00:0:0:1::99", 64)],
            known: ["[fd00:0:0:1::7]:50001", "FD00:0:0:2::1:20", "[fd00::3:4]", "fd00::1.2.3.4"]
        )
        // fd00:0:0:1::7 is inside the phone's own /120 already; the first longest zero run is the one shortened.
        XCTAssertEqual(Array(networks.prefix(4)), ["fd00:0:0:1::/120", "fd00::2:0:0:1:0/120", "fd00::3:0/120", "fd00::102:300/120"])
        XCTAssertEqual(networks.count, 4 + commonPrivateNetworks.count)
    }

    func testMalformedIpv6IsIgnored() {
        let known = ["fd00:::1", "fd00::1::2", "fd00:12345::1", "fd00:g::1", "1:2:3:4:5:6:7:8:9", "fd00:1:2:3:4:5:6", "fd00::1.2.3.4:5"]
        XCTAssertEqual(scanNetworks(local: [], known: known), commonPrivateNetworks)
    }

    func testWideLocalNetworkIsNarrowedAroundThePhone() {
        XCTAssertEqual(scanNetworks(local: [LocalAddress("10.5.130.9", 16)], known: []).first, "10.5.128.0/22")
    }

    func testPublicAndCoveredNetworksAreSkipped() {
        let networks = scanNetworks(
            local: [LocalAddress("192.168.0.0", 23), LocalAddress("8.8.8.8", 24)],
            known: ["192.168.1.7"]
        )
        // 192.168.0.0/24 and 192.168.1.0/24 are inside the phone's /23.
        XCTAssertEqual(networks.first, "192.168.0.0/23")
        XCTAssertFalse(networks.contains { $0.hasPrefix("8.8.") })
        XCTAssertFalse(networks.contains("192.168.1.0/24"))
        XCTAssertFalse(networks.contains("192.168.0.0/24"))
    }

    func testScanStaysWithinTheHostLimit() {
        let local = (0..<8).map { LocalAddress("10.\($0).0.1", 22) }
        let networks = scanNetworks(local: local, known: [])
        let hosts = networks.reduce(0) { $0 + (1 << (32 - Int($1.split(separator: "/")[1])!)) }
        XCTAssertLessThanOrEqual(hosts, maxScanHosts)
        XCTAssertEqual(networks.count, 4)
    }

    func testCgnatCountsAsPrivate() {
        XCTAssertEqual(scanNetworks(local: [LocalAddress("100.64.3.9", 24)], known: []).first, "100.64.3.0/24")
    }

    func testHostOfDropsThePort() {
        XCTAssertEqual(hostOf("10.0.0.1:50000"), "10.0.0.1")
        XCTAssertEqual(hostOf("10.0.0.1"), "10.0.0.1")
        XCTAssertEqual(hostOf("fd00::1"), "fd00::1")
        XCTAssertEqual(hostOf("node.lan:50001"), "node.lan")
        XCTAssertEqual(hostOf("[fd00::1]:50000"), "fd00::1")
    }

    func testEndpointRejectsSeparators() {
        XCTAssertTrue(isEndpoint("10.0.0.1:50000"))
        XCTAssertTrue(isEndpoint("node-1.lan"))
        XCTAssertFalse(isEndpoint(""))
        XCTAssertFalse(isEndpoint("10.0.0.1, 10.0.0.2"))
        XCTAssertFalse(isEndpoint("a b"))
        XCTAssertFalse(isEndpoint(String(repeating: "a", count: endpointMax + 1)))
    }

    func testMatchesDecodeGoNulls() throws {
        let json = #"[{"endpoint":"10.0.0.2","hostname":"cp-1","version":"v1.11.0","role":"controlplane","contexts":null}]"#
        let matches = try JSONDecoder().decode([EndpointMatch].self, from: Data(json.utf8))
        XCTAssertEqual(matches, [EndpointMatch(endpoint: "10.0.0.2", hostname: "cp-1", version: "v1.11.0", role: "controlplane")])
        let probe = try JSONDecoder().decode(EndpointProbe.self, from: Data(#"{"endpoint":"10.0.0.2","hostname":"cp-1","version":""}"#.utf8))
        XCTAssertEqual(probe.summary, "cp-1")
    }
}

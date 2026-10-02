import XCTest
@testable import IchorCore

final class CaptureTests: XCTestCase {
    func testSummaryDecoding() throws {
        let json = #"{"n":3,"ts":1800000000123,"len":74,"src":"192.0.2.1:53","dst":"192.0.2.9:41000","proto":"DNS","info":"A example.org"}"#
        let packet = try TalosJSON.decode(PacketSummary.self, from: json)
        XCTAssertEqual(packet, PacketSummary(n: 3, ts: 1_800_000_000_123, len: 74, src: "192.0.2.1:53",
                                             dst: "192.0.2.9:41000", proto: "DNS", info: "A example.org"))
        XCTAssertEqual(packet.protoKind, .dns)

        let sparse = try TalosJSON.decode(PacketSummary.self, from: #"{"n":1,"info":null}"#)
        XCTAssertEqual(sparse, PacketSummary(n: 1))
    }

    func testPageAndDetailDecoding() throws {
        let page = try TalosJSON.decode(PcapPage.self, from: #"{"packets":[{"n":1},{"n":2}],"total":1200}"#)
        XCTAssertEqual(page.packets.map(\.n), [1, 2])
        XCTAssertEqual(page.total, 1200)
        let empty = try TalosJSON.decode(PcapPage.self, from: #"{"packets":null}"#)
        XCTAssertEqual(empty, PcapPage(packets: [], total: 0))

        let detail = try TalosJSON.decode(PacketDetail.self, from: """
        {"layers":[{"name":"Ethernet","fields":[{"k":"Src","v":"aa:bb"}]},{"name":"Payload","fields":null}],"hex":"0000  aa bb"}
        """)
        XCTAssertEqual(detail.layers.map(\.name), ["Ethernet", "Payload"])
        XCTAssertEqual(detail.layers[0].fields, [PacketDetail.Field(k: "Src", v: "aa:bb")])
        XCTAssertEqual(detail.layers[1].fields, [])
        XCTAssertEqual(detail.hex, "0000  aa bb")
    }

    func testProtoKind() {
        XCTAssertEqual(ProtoKind("TCP"), .tcp)
        XCTAssertEqual(ProtoKind("udp"), .udp)
        XCTAssertEqual(ProtoKind("ICMPv6"), .icmp)
        XCTAssertEqual(ProtoKind("ARP"), .arp)
        XCTAssertEqual(ProtoKind("TLS"), .tls)
        XCTAssertEqual(ProtoKind("VRRP"), .other)
    }

    func testOptionsValidation() {
        var options = CaptureOptions()
        XCTAssertEqual(captureOptionsProblem(options, validatedFilter: nil, filterError: ""), .noInterface)
        options.interface = "eth0"
        XCTAssertNil(captureOptionsProblem(options, validatedFilter: nil, filterError: ""), "an empty filter captures everything")
        options.filter = " tcp port 443 "
        XCTAssertEqual(captureOptionsProblem(options, validatedFilter: nil, filterError: ""), .checkingFilter)
        XCTAssertEqual(captureOptionsProblem(options, validatedFilter: "tcp port", filterError: ""), .checkingFilter)
        XCTAssertNil(captureOptionsProblem(options, validatedFilter: "tcp port 443", filterError: ""))
        XCTAssertEqual(captureOptionsProblem(options, validatedFilter: "tcp port 443", filterError: "syntax error"),
                       .invalidFilter("syntax error"))
    }

    func testDefaults() {
        let options = CaptureOptions()
        XCTAssertFalse(options.promiscuous)
        XCTAssertEqual(options.duration.seconds, 30)
        XCTAssertEqual(options.sizeLimit.bytes, 20 * 1_048_576)
        XCTAssertEqual(CaptureDuration.allCases.map(\.seconds), [10, 30, 60, 300])
        XCTAssertEqual(CaptureSizeLimit.allCases.map(\.bytes), [5_242_880, 20_971_520, 104_857_600])
        XCTAssertEqual(CapturePreset.all.map(\.filter),
                       ["udp port 53", "icmp or icmp6", "tcp port 443", "tcp port 6443"])
        XCTAssertEqual(options.snapLen, 0)
        XCTAssertEqual(PacketSummary(n: 0).number, 1)
    }

    func testInterfaces() {
        let links = [
            NetLink(name: "lxc123", kind: "veth", virtual: true),
            NetLink(name: "lo", type: "loopback"),
            NetLink(name: "eth1", state: "down"),
            NetLink(name: "bond0", kind: "bond"),
            NetLink(name: "eth0"),
        ]
        XCTAssertEqual(captureInterfaces(links, includeVirtual: false).map(\.name), ["eth0", "bond0", "eth1", "lo"])
        XCTAssertEqual(captureInterfaces(links, includeVirtual: true).map(\.name), ["eth0", "bond0", "eth1", "lxc123", "lo"])
    }

    func testFilename() {
        let date = Date(timeIntervalSince1970: 1_800_000_005) // 2027-01-15 08:00:05 UTC
        let utc = TimeZone(identifier: "UTC")!
        XCTAssertEqual(captureFilename(hostname: "cp-1", interface: "eth0", date: date, timeZone: utc),
                       "cp-1-eth0-20270115-080005.pcap")
        XCTAssertEqual(captureFilename(hostname: "a b/c", interface: "", date: date, timeZone: utc),
                       "a-b-c-any-20270115-080005.pcap")
        XCTAssertEqual(captureFilename(hostname: "nœud", interface: "eth0.10", date: date, timeZone: utc),
                       "n-ud-eth0.10-20270115-080005.pcap")
    }

    func testFilesAndPaging() {
        let old = CaptureFile(name: "a.pcap", size: 10, modified: Date(timeIntervalSince1970: 1))
        let new = CaptureFile(name: "b.pcap", size: 30, modified: Date(timeIntervalSince1970: 2))
        let other = CaptureFile(name: "notes.txt", size: 5, modified: Date(timeIntervalSince1970: 3))
        XCTAssertEqual(sortCaptureFiles([old, other, new]).map(\.name), ["b.pcap", "a.pcap"])
        XCTAssertEqual(totalCaptureSize([old, new]), 40)
        XCTAssertEqual(pcapPageOffsets(total: 0), [0])
        XCTAssertEqual(pcapPageOffsets(total: 500), [0])
        XCTAssertEqual(pcapPageOffsets(total: 1001), [0, 500, 1000])
    }

    func testLiveListKeepsNewest() {
        let list = (1...4).map { PacketSummary(n: $0) }
        XCTAssertEqual(appendPackets(list, [PacketSummary(n: 5)], limit: 3).map(\.n), [3, 4, 5])
        XCTAssertEqual(appendPackets([], [PacketSummary(n: 1)], limit: 3).map(\.n), [1])
        XCTAssertEqual(formatElapsed(65), "01:05")
        XCTAssertEqual(formatElapsed(-3), "00:00")
    }

    func testRoles() {
        XCTAssertTrue(ContextSummary(name: "o", roles: ["os:operator"]).allows(.packetCapture))
        XCTAssertFalse(ContextSummary(name: "r", roles: ["os:reader"]).allows(.packetCapture))
        XCTAssertFalse(ContextSummary(name: "o", roles: ["os:operator"]).allows(.upgrade))
        XCTAssertTrue(ContextSummary(name: "a", roles: ["os:admin"]).allows(.upgrade))
    }
}

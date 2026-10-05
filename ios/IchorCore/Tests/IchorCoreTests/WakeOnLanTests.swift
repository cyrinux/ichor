import XCTest
@testable import IchorCore

final class WakeOnLanTests: XCTestCase {
    private let mac: [UInt8] = [0xAA, 0xBB, 0xCC, 0x01, 0x02, 0x03]

    func testMacsParseInTheUsualNotations() {
        for text in ["aa:bb:cc:01:02:03", "AA-BB-CC-01-02-03", "aabb.cc01.0203", "aabbcc010203", " aa:bb:cc:01:02:03 "] {
            XCTAssertEqual(parseMac(text), mac, text)
        }
    }

    func testNotAMac() {
        for text in ["", "aa:bb:cc:01:02", "aa:bb:cc:01:02:03:04", "gg:bb:cc:01:02:03", "10.0.0.1"] {
            XCTAssertNil(parseMac(text), text)
        }
    }

    func testMacsAreWrittenLowercaseWithColons() {
        XCTAssertEqual(formatMac(mac), "aa:bb:cc:01:02:03")
    }

    func testMagicPacketIsSyncThenSixteenMacs() {
        let packet = magicPacket(mac)
        XCTAssertEqual(packet.count, 102)
        XCTAssertTrue(packet.prefix(6).allSatisfy { $0 == 0xFF })
        for i in 0..<16 { XCTAssertEqual(Array(packet[(6 + i * 6)..<(12 + i * 6)]), mac) }
    }

    func testBroadcastIsOptionalIpv4OrHostName() {
        for text in ["", "  ", "192.168.1.255", "255.255.255.255", "wol-relay.lan", "router"] { XCTAssertTrue(isWolAddress(text), text) }
        for text in ["192.168.1.256", "192.168.1", "1.2.3.4.5", "bad host", "-x.lan", "a|b"] { XCTAssertFalse(isWolAddress(text), text) }
    }

    func testDirectedBroadcastOfTheSubnet() {
        let ip: [UInt8] = [192, 168, 1, 20]
        XCTAssertEqual(directedBroadcast(ip, prefix: 24), "192.168.1.255")
        XCTAssertEqual(directedBroadcast(ip, prefix: 30), "192.168.1.23")
        XCTAssertEqual(directedBroadcast(ip, prefix: 0), "255.255.255.255")
        XCTAssertEqual(directedBroadcast(ip, prefix: 32), "192.168.1.20")
        XCTAssertNil(directedBroadcast(ip, prefix: 33))
        XCTAssertNil(directedBroadcast([UInt8](repeating: 0, count: 16), prefix: 64))
    }

    func testTypedFieldsBecomeATarget() throws {
        XCTAssertEqual(try parseWolTarget(mac: "AA-BB-CC-01-02-03", broadcast: " 192.168.1.255 ", port: "7").get(),
                       WolTarget(mac: "aa:bb:cc:01:02:03", broadcast: "192.168.1.255", port: 7))
        let defaults = try parseWolTarget(mac: "aabbcc010203", broadcast: "", port: "").get()
        XCTAssertEqual(defaults.port, wolDefaultPort)
        XCTAssertEqual(defaults.address, wolDefaultBroadcast)
    }

    func testBadFieldsSayWhichOne() {
        func error(_ mac: String, _ broadcast: String, _ port: String) -> WolInputError? {
            if case .failure(let error) = parseWolTarget(mac: mac, broadcast: broadcast, port: port) { return error }
            return nil
        }
        XCTAssertEqual(error("nope", "", ""), .mac)
        XCTAssertEqual(error("aabbcc010203", "300.1.1.1", ""), .broadcast)
        XCTAssertEqual(error("aabbcc010203", "", "0"), .port)
        XCTAssertEqual(error("aabbcc010203", "", "70000"), .port)
    }

    func testStoredFormRoundTrips() {
        let target = WolTarget(mac: "aa:bb:cc:01:02:03", broadcast: "10.0.0.255", port: 7)
        XCTAssertEqual(decodeWolTarget(encodeWolTarget(target)), target)
        XCTAssertEqual(decodeWolTarget(encodeWolTarget(WolTarget(mac: "aa:bb:cc:01:02:03"))), WolTarget(mac: "aa:bb:cc:01:02:03"))
        for text in ["", "aa:bb:cc:01:02:03", "nope||9", "aabbcc010203||x", "aabbcc010203||0"] { XCTAssertNil(decodeWolTarget(text), text) }
    }

    func testNodesOfRemovedClustersAreForgotten() {
        let target = WolTarget(mac: "aa:bb:cc:01:02:03")
        let saved = [wolKey(fingerprint: "fp-prod", node: "10.0.0.1"): target, wolKey(fingerprint: "fp-old", node: "10.0.0.2"): target]
        XCTAssertEqual(keepWolTargets(saved, fingerprints: ["fp-prod", ""]), [wolKey(fingerprint: "fp-prod", node: "10.0.0.1"): target])
    }

    func testSeenMacsRoundTrip() {
        let seen = [SeenMac(link: "eth0", mac: "aa:bb:cc:01:02:03"), SeenMac(link: "enp1s0", mac: "aa:bb:cc:01:02:04")]
        XCTAssertEqual(decodeSeenMacs(encodeSeenMacs(seen)), seen)
        XCTAssertEqual(decodeSeenMacs("eth0=AA-BB-CC-01-02-03,bad,=aabbcc010203,eth1=nope"), [SeenMac(link: "eth0", mac: "aa:bb:cc:01:02:03")])
        XCTAssertEqual(decodeSeenMacs(""), [])
    }

    func testSeenMacsAreThePhysicalLinksNormalized() {
        let links = [
            NetLink(name: "eth0", type: "ether", hardwareAddr: "AA:BB:CC:01:02:03"),
            NetLink(name: "lxc1", type: "ether", hardwareAddr: "aa:bb:cc:01:02:05", virtual: true),
        ]
        XCTAssertEqual(seenMacs(links), [SeenMac(link: "eth0", mac: "aa:bb:cc:01:02:03")])
    }

    func testWakeUsesTheSavedSettingElseEverySeenMac() {
        let saved = WolTarget(mac: "aa:bb:cc:01:02:03", broadcast: "10.0.0.255", port: 7)
        let seen = [SeenMac(link: "eth0", mac: "aa:bb:cc:01:02:03"), SeenMac(link: "eth1", mac: "aa:bb:cc:01:02:04"),
                    SeenMac(link: "eth2", mac: "aa:bb:cc:01:02:04")]
        XCTAssertEqual(wakeTargets(saved: saved, seen: seen), [saved])
        XCTAssertEqual(wakeTargets(saved: nil, seen: seen), [WolTarget(mac: "aa:bb:cc:01:02:03"), WolTarget(mac: "aa:bb:cc:01:02:04")])
        XCTAssertEqual(wakeTargets(saved: nil, seen: []), [])
    }

    func testCandidatesArePhysicalEthernetLinks() {
        let links = [
            NetLink(name: "eth0", type: "ether", hardwareAddr: "aa:bb:cc:01:02:03"),
            NetLink(name: "eth1", type: "ether", hardwareAddr: "aa:bb:cc:01:02:04"),
            NetLink(name: "bond0", type: "ether", kind: "bond", hardwareAddr: "aa:bb:cc:01:02:03"),
            NetLink(name: "lxc1", type: "ether", hardwareAddr: "aa:bb:cc:01:02:05", virtual: true),
            NetLink(name: "lo", type: "loopback", hardwareAddr: "00:00:00:00:00:00"),
            NetLink(name: "wg0", type: "none", kind: "wireguard"),
        ]
        XCTAssertEqual(wolCandidates(links).map(\.name), ["eth0", "eth1"])
    }
}

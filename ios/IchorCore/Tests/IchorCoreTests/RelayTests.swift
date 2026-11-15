import XCTest
@testable import IchorCore

final class RelayTests: XCTestCase {
    func testDecodesTheGoDiscovery() throws {
        let json = #"""
            {"sources":[{"mode":"proxy","kind":"ichor-relay","namespace":"ichor-relay","service":"ichor-relay","port":8080}],
             "canPair":false,"reason":"pairing needs get on the Secret ichor-relay/ichor-relay"}
            """#
        let found = try TalosJSON.decode(RelayDiscovery.self, from: json)
        XCTAssertEqual(found.sources.map(\.service), ["ichor-relay"])
        XCTAssertFalse(found.canPair)
        XCTAssertTrue(found.reason.hasPrefix("pairing needs get"))
        XCTAssertEqual(try TalosJSON.decode(RelayDiscovery.self, from: "{}"), RelayDiscovery())
    }

    func testEncodesTheDeviceGoReads() throws {
        let device = RelayDevice(
            platform: RelayDevice.ntfy, endpoint: "https://ntfy.sh/abc", label: "homelab", verbose: true)
        let object = try XCTUnwrap(
            JSONSerialization.jsonObject(with: JSONEncoder().encode(device)) as? [String: Any])
        XCTAssertEqual(object["platform"] as? String, "ntfy")
        XCTAssertEqual(object["endpoint"] as? String, "https://ntfy.sh/abc")
        XCTAssertEqual(object["label"] as? String, "homelab")
        XCTAssertEqual(object["verbose"] as? Bool, true)
        XCTAssertEqual(Set(object.keys), ["platform", "endpoint", "label", "verbose"])
    }

    func testPairingRecordRoundTrips() throws {
        let source = PromSource(kind: "ichor-relay", namespace: "ichor-relay", service: "ichor-relay", port: 8080)
        let record = RelayPairingRecord(
            deviceId: "0123456789abcdef", endpoint: "https://ntfy.sh/abc", source: source,
            pairedAt: Date(timeIntervalSince1970: 1_800_000_000))
        let decoded = try JSONDecoder().decode(RelayPairingRecord.self, from: JSONEncoder().encode(record))
        XCTAssertEqual(decoded, record)
    }

    func testTopicURL() {
        let long64 = String(repeating: "a", count: 64)
        let cases: [(server: String, topic: String, want: String?)] = [
            ("https://ntfy.sh", "abcDEF012_-", "https://ntfy.sh/abcDEF012_-"),
            ("https://ntfy.sh/", "abc", "https://ntfy.sh/abc"),
            (" https://push.example.org:8443 ", " abc ", "https://push.example.org:8443/abc"),
            ("HTTPS://ntfy.sh", "abc", "https://ntfy.sh/abc"),
            ("http://ntfy.sh", "abc", nil),
            ("ntfy.sh", "abc", nil),
            ("https://", "abc", nil),
            ("https://ntfy.sh/sub", "abc", nil),
            ("https://ntfy.sh?x=1", "abc", nil),
            ("https://ntfy.sh#x", "abc", nil),
            ("https://user:pass@ntfy.sh", "abc", nil),
            ("https://ntfy.sh", "", nil),
            ("https://ntfy.sh", "a/b", nil),
            ("https://ntfy.sh", "a b", nil),
            ("https://ntfy.sh", "é", nil),
            ("https://ntfy.sh", long64, "https://ntfy.sh/" + long64),
            ("https://ntfy.sh", long64 + "a", nil),
        ]
        for c in cases {
            XCTAssertEqual(ntfyTopicURL(server: c.server, topic: c.topic)?.absoluteString, c.want, "\(c)")
        }
    }

    func testRandomTopicIsValid() {
        let topics = (0..<20).map { _ in randomNtfyTopic() }
        XCTAssertTrue(topics.allSatisfy { $0.count == 16 && isNtfyTopic($0) })
        XCTAssertGreaterThan(Set(topics).count, 1)
    }

    func testAppURLAndLabel() throws {
        let url = try XCTUnwrap(ntfyTopicURL(server: "https://push.example.org:8443", topic: "abc"))
        XCTAssertEqual(ntfyAppURL(url)?.absoluteString, "ntfy://push.example.org:8443/abc")
        XCTAssertEqual(ntfyTopicLabel(url.absoluteString), "push.example.org:8443/abc")
        XCTAssertEqual(ntfyTopicLabel("https://ntfy.sh/abc"), "ntfy.sh/abc")
    }
}

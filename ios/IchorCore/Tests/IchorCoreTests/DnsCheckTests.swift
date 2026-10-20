import XCTest
@testable import IchorCore

/// The wire format of go/ichorgo/dns_check.go: what the apps read back and what they send in.
final class DnsCheckTests: XCTestCase {
    private let json = #"""
    {"results":[
     {"dnsName":"app.example.com","status":"match","answers":["203.0.113.10"],"owner":"homelab","ownerMatch":true,"ms":12,"message":""},
     {"dnsName":"db.example.com","status":"mismatch","answers":["203.0.113.99"],"owner":"homelab","ownerMatch":false,"ms":9,"message":""},
     {"dnsName":"gone.example.com","status":"missing","answers":[],"owner":"","ownerMatch":null,"ms":7,"message":""},
     {"dnsName":"broken.example.com","status":"error","answers":[],"owner":"","ownerMatch":null,"ms":0,"message":"lookup failed"}],
     "resolver":"system"}
    """#

    func testDecodesTheVerdictsAndTheOwnershipAnswer() throws {
        let r = try TalosJSON.decode(DnsCheckResult.self, from: json)
        XCTAssertEqual(r.resolver, "system")
        XCTAssertEqual(r.results.map(\.verdict), [.match, .mismatch, .missing, .error])
        // true: the TXT registry record names this cluster; false: another owner owns it.
        XCTAssertEqual(r.results[0].ownerMatch, true)
        XCTAssertEqual(r.results[0].owner, "homelab")
        XCTAssertEqual(r.results[1].ownerMatch, false)
        // No readable TXT record: nil, so the row says "owner unknown" rather than not ours.
        XCTAssertNil(r.results[2].ownerMatch)
        XCTAssertEqual(r.results[2].answers, [])
        XCTAssertEqual(r.results[0].ms, 12)
        XCTAssertEqual(r.results[3].message, "lookup failed")
    }

    func testAWildcardCarriesTheNameItWasCheckedAs() throws {
        let wire = #"""
        {"results":[
         {"dnsName":"*.cdn.example.com","probed":"ichor-dns-check.cdn.example.com","status":"match","answers":["203.0.113.10"]},
         {"dnsName":"app.example.com","status":"match"}],
         "resolver":"system"}
        """#
        let r = try TalosJSON.decode(DnsCheckResult.self, from: wire)
        // The verdict stays keyed by the wildcard; probed is the sample label actually resolved.
        XCTAssertEqual(r.results[0].dnsName, "*.cdn.example.com")
        XCTAssertEqual(r.results[0].probed, "ichor-dns-check.cdn.example.com")
        // An older core sends no probed field: a plain name reads "".
        XCTAssertEqual(r.results[1].probed, "")
    }

    func testANameWithNoTargetResolvesUnverified() throws {
        let wire = #"""
        {"results":[{"dnsName":"status.example.com","status":"unverified","answers":["203.0.113.99"],"owner":"homelab","ownerMatch":true}],
         "resolver":"system"}
        """#
        let r = try TalosJSON.decode(DnsCheckResult.self, from: wire)
        // It resolves, but nothing to compare with: never a mismatch, and the answers stay shown.
        XCTAssertEqual(r.results[0].verdict, .unverified)
        XCTAssertEqual(r.results[0].answers, ["203.0.113.99"])
    }

    func testAStatusFromANewerCoreReadsAsNoVerdict() throws {
        let wire = #"{"results":[{"dnsName":"a.example.com","status":"someday"}],"resolver":"system"}"#
        let r = try TalosJSON.decode(DnsCheckResult.self, from: wire)
        // The row shows its targets and no verdict, never a guessed one.
        XCTAssertNil(r.results[0].verdict)
        XCTAssertEqual(r.results[0].answers, [])
        XCTAssertEqual(r.results[0].message, "")
        XCTAssertNil(r.results[0].ownerMatch)
    }

    func testTheRequestCarriesTheFieldNamesGoReads() throws {
        let encoded = try TalosJSON.encode(DnsCheckRequest(dnsName: "app.example.com", recordType: "A",
                                                           targets: ["203.0.113.10"], ownerId: "homelab", txtPrefix: "txt."))
        let sent = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(encoded.utf8)) as? [String: Any])
        XCTAssertEqual(sent.keys.sorted(), ["dnsName", "ownerId", "recordType", "targets", "txtPrefix"])
        XCTAssertEqual(sent["dnsName"] as? String, "app.example.com")
        XCTAssertEqual(sent["recordType"] as? String, "A")
        XCTAssertEqual(sent["ownerId"] as? String, "homelab")
        XCTAssertEqual(sent["txtPrefix"] as? String, "txt.")
        XCTAssertTrue(encoded.contains(#""targets":["203.0.113.10"]"#))
    }

    func testARecordNoControllerCoversIsStillSent() throws {
        // An unmanaged name has no owner id or TXT prefix; the Go core skips its ownership check.
        let encoded = try TalosJSON.encode(DnsCheckRequest(dnsName: "*.example.net", recordType: "A", targets: []))
        let sent = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(encoded.utf8)) as? [String: Any])
        XCTAssertEqual(sent.keys.sorted(), ["dnsName", "ownerId", "recordType", "targets", "txtPrefix"])
        XCTAssertEqual(sent["ownerId"] as? String, "")
        XCTAssertEqual(sent["txtPrefix"] as? String, "")
    }
}

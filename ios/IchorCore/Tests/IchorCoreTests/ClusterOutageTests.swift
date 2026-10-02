import XCTest
@testable import IchorCore

final class ClusterOutageTests: XCTestCase {
    private func overview(_ nodes: [String]) throws -> ClusterOverview {
        try TalosJSON.decode(ClusterOverview.self, from: #"{"context":"lab","nodes":[\#(nodes.joined(separator: ","))]}"#)
    }

    private func up(_ addr: String) -> String {
        #"{"node":"\#(addr)","hostname":"\#(addr)","reachable":true,"version":"v1.14.1","arch":"amd64","platform":"metal","role":"worker","stage":"running","ready":true,"unmetConditions":[]}"#
    }

    private func down(_ addr: String, kind: String? = "network", error: String = "unreachable: no route to host") -> String {
        let kindField = kind.map { #","errorKind":"\#($0)""# } ?? ""
        return #"{"node":"\#(addr)","hostname":"\#(addr)","reachable":false,"error":"\#(error)"\#(kindField),"version":"","arch":"","platform":"","role":"unknown","stage":"unknown","ready":false,"unmetConditions":[]}"#
    }

    func testAnyNodeAnsweringIsNoOutage() throws {
        XCTAssertNil(try overview([up("a"), down("b")]).outage)
        XCTAssertNil(try overview([]).outage)
    }

    func testAllNetworkFailuresMeanOffTheClusterNetwork() throws {
        let outage = try overview([down("a"), down("b", error: "timed out")]).outage
        XCTAssertEqual(outage?.cause, .network)
        XCTAssertEqual(outage?.nodes, 2)
    }

    func testCertificateOrRoleFailuresAreCredentials() throws {
        XCTAssertEqual(try overview([down("a", kind: "tls"), down("b", kind: "auth")]).outage?.cause, .credentials)
    }

    func testMixedOrUnknownKindsAreOther() throws {
        XCTAssertEqual(try overview([down("a"), down("b", kind: "auth")]).outage?.cause, .other)
        XCTAssertEqual(try overview([down("a", kind: nil)]).outage?.cause, .other)
    }

    func testTheSameErrorFromEveryNodeIsShownOnce() throws {
        let outage = try overview([down("a"), down("b"), down("c", error: "unreachable: connection refused")]).outage
        XCTAssertEqual(outage?.errors, ["unreachable: no route to host", "unreachable: connection refused"])
    }
}

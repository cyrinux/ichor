import XCTest
@testable import IchorCore

final class TalosPatchTests: XCTestCase {
    private func node(_ address: String, _ hostname: String) -> NodeOverview {
        NodeOverview(node: address, hostname: hostname, reachable: true)
    }

    func testDecodesTheCoreAnswer() throws {
        let json =
            #"{"talos":true,"kind":"documents","documents":[{"kind":"HostnameConfig","hostname":"cp-1"},{"kind":"v1alpha1"}],"touchesSecrets":false,"tryable":true}"#
        let info = try JSONDecoder().decode(TalosPatchInfo.self, from: Data(json.utf8))
        XCTAssertTrue(info.tryable)
        XCTAssertEqual(info.kind, "documents")
        XCTAssertEqual(info.hostnames, ["cp-1"])

        let other = try JSONDecoder().decode(TalosPatchInfo.self, from: Data(#"{"talos":false}"#.utf8))
        XCTAssertFalse(other.talos)
        XCTAssertTrue(other.documents.isEmpty)
    }

    func testTheNodeThePatchIsFor() {
        let nodes = [node("192.0.2.11", "cp-1.lab.example.test"), node("192.0.2.21", "worker-1")]
        func info(_ hosts: String...) -> TalosPatchInfo {
            TalosPatchInfo(
                talos: true, documents: hosts.map { TalosPatchDoc(kind: "HostnameConfig", hostname: $0) }, tryable: true
            )
        }
        XCTAssertEqual(info("worker-1").node(in: nodes)?.node, "192.0.2.21")
        XCTAssertEqual(info("cp-1").node(in: nodes)?.node, "192.0.2.11")
        XCTAssertEqual(info("CP-1.lab.example.test").node(in: nodes)?.node, "192.0.2.11")
        XCTAssertNil(info().node(in: nodes))
        XCTAssertNil(info("cp-9").node(in: nodes))
        XCTAssertEqual(info("cp-1", "cp-1").hostnames, ["cp-1"])
    }
}

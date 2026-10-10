import XCTest
@testable import IchorCore

final class KubeSpanDiagnosticsTests: XCTestCase {
    func testDecoding() throws {
        let all = try TalosJSON.decode(KubeSpanDiagAll.self, from: """
        {"nodes":[
          {"node":"10.0.0.1","hostname":"cp-1","config":{"enabled":true,"mtu":1420,"endpointFilters":null},"linkMtu":1420,
           "peers":[{"publicKey":"k2","label":"w-1","state":"down","address":"fd00::2","allowedIPs":["fd00::2/128"],
             "endpointsTried":["203.0.113.5:51820"],"endpoint":"","lastUsedEndpoint":"","lastHandshake":0,"lastEndpointChange":0,
             "verdicts":[{"kind":"staleHandshake","message":"no handshake ever completed"}]}],
           "siderolink":{"host":"omni.example.invalid:8090","connected":true,"linkName":"siderolink","grpcTunnel":false},
           "errors":{}},
          {"node":"10.0.0.2","hostname":"w-1","config":null,"linkMtu":0,"peers":null,"siderolink":null,"errors":{"config":"unreachable"}}
        ]}
        """)
        let peer = try XCTUnwrap(all.peer(node: "10.0.0.1", publicKey: "k2"))
        XCTAssertEqual(peer.verdicts.map(\.kind), ["staleHandshake"])
        XCTAssertEqual(peer.endpointsTried, ["203.0.113.5:51820"])
        XCTAssertEqual(all.node("10.0.0.1")?.config?.mtu, 1420)
        XCTAssertEqual(all.node("10.0.0.1")?.config?.endpointFilters, [])
        XCTAssertEqual(all.node("10.0.0.1")?.siderolink?.connected, true)
        XCTAssertNil(all.node("10.0.0.2")?.siderolink)
        XCTAssertEqual(all.node("10.0.0.2")?.peers, [])
        XCTAssertEqual(all.node("10.0.0.2")?.errors["config"], "unreachable")
        XCTAssertNil(all.peer(node: "10.0.0.9", publicKey: "k2"))
    }
}

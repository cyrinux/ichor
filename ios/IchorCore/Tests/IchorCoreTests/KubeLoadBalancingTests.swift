import XCTest
@testable import IchorCore

final class KubeLoadBalancingTests: XCTestCase {
    private let json = #"""
    {"controller":"metallb","controllers":["metallb","kube-vip"],
     "pools":[{"name":"default-pool","controller":"metallb","addresses":["192.0.2.240/31"],"total":2,"assigned":2,"available":0,
               "countsKnown":true,"autoAssign":true,"advertisedBy":["l2:default"],"services":["dns/blocky"],"level":"critical"},
              {"name":"lab-pool","controller":"metallb","total":8,"available":8,"autoAssign":false,"level":"warning"}],
     "advertisements":[{"name":"default","kind":"l2","pools":["default-pool"],"interfaces":["eth0"]}],
     "bgpPeers":[{"name":"upstream-router","peerAddress":"192.0.2.1","peerASN":64512,"myASN":64513,
                  "sessions":[{"node":"demo-worker-2","up":false}],"sessionsKnown":true,"level":"critical"}],
     "configErrors":[],
     "services":[{"namespace":"demo","name":"hello-ichor-public","addresses":[],"pending":true,"cause":"poolExhausted","since":1,"level":"critical"},
                 {"namespace":"edge","name":"next","pending":true,"cause":"somethingNewer","level":"newer-level"},
                 {"namespace":"networking","name":"traefik","addresses":["192.0.2.240"],"pool":"default-pool","announcedBy":"demo-worker-1","level":"ok"}],
     "kubeVip":{"ranges":[{"scope":"global","cidrs":["198.51.100.64/29"]}],"leases":[{"name":"plndr-cp-lock","namespace":"kube-system","holder":"demo-cp-1"}]},
     "partialAccess":true}
    """#

    private func decode(_ text: String) throws -> KubeLoadBalancing {
        try JSONDecoder().decode(KubeLoadBalancing.self, from: Data(text.utf8))
    }

    func testDecodesTheCoreJSON() throws {
        let lb = try decode(json)
        XCTAssertEqual(lb.controllers, ["metallb", "kube-vip"])
        XCTAssertTrue(lb.partialAccess)
        XCTAssertEqual(lb.pools[0].level, .critical)
        XCTAssertEqual(lb.pools[0].used, 1)
        XCTAssertFalse(lb.pools[1].autoAssign)
        XCTAssertEqual(lb.pools[1].advertisedBy, [])
        XCTAssertEqual(lb.bgpPeers.first?.sessions.first?.up, false)
        XCTAssertEqual(lb.kubeVip?.ranges.first?.scope, "global")
        XCTAssertEqual(lb.kubeVip?.leases.first?.renewed, 0)
        XCTAssertFalse(lb.isEmpty)
    }

    func testPendingServicesKeepTheirCauseAndUnknownOnesFallBack() throws {
        let pending = try decode(json).pending
        XCTAssertEqual(pending.map(\.id), ["demo/hello-ichor-public", "edge/next"])
        XCTAssertEqual(pending[0].cause, .poolExhausted)
        XCTAssertEqual(pending[1].cause, .unknown)
        XCTAssertEqual(pending[1].level, .ok)
    }

    func testWithoutControllerOrServiceItIsEmpty() throws {
        let lb = try decode(#"{"controllers":[],"services":[],"kubeVip":null}"#)
        XCTAssertTrue(lb.isEmpty)
        XCTAssertNil(lb.kubeVip)
    }

    func testKubeVipIsAKnownController() {
        XCTAssertEqual(LBController(rawValue: "kube-vip"), .kubeVip)
    }
}

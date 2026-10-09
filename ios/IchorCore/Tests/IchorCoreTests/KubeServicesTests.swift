import XCTest
@testable import IchorCore

final class KubeServicesTests: XCTestCase {
    private let json = #"""
    {"partialAccess":true,"lbController":"metallb","services":[
     {"namespace":"shop","name":"web-public","type":"LoadBalancer","clusterIP":"10.96.0.11","ports":["443:30443/TCP"],
      "addresses":[],"pending":true,"selector":true,"endpointsKnown":true,"endpoints":2,"readyEndpoints":2,"routes":[],"level":"critical"},
     {"namespace":"shop","name":"web","type":"ClusterIP","clusterIP":"10.96.0.10","ports":["80/TCP"],"addresses":[],
      "selector":true,"endpointsKnown":true,"endpoints":3,"readyEndpoints":2,
      "routes":[{"kind":"Ingress","namespace":"shop","name":"web","url":"https://shop.example.org","service":"web"}],"level":"warning"},
     {"namespace":"shop","name":"queue","type":"ClusterIP","clusterIP":"None","level":"newer-level"},
     {"namespace":"shop","name":"legacy","type":"ExternalName","externalName":"db.example.net","endpointsKnown":true}]}
    """#

    private func services() throws -> KubeServices {
        try JSONDecoder().decode(KubeServices.self, from: Data(json.utf8))
    }

    func testDecodesTheCoreJSON() throws {
        let s = try services()
        XCTAssertTrue(s.partialAccess)
        XCTAssertTrue(s.anyPending)
        XCTAssertEqual(LBController(rawValue: s.lbController), .metallb)
        let pub = try XCTUnwrap(s.services.first)
        XCTAssertEqual(pub.id, "shop/web-public")
        XCTAssertEqual(pub.level, .critical)
        XCTAssertEqual(s.services[1].routes.first?.label, "shop.example.org")
        // An older or partial row decodes with its defaults; an unknown level reads as ok.
        XCTAssertEqual(s.services[2].level, .ok)
        XCTAssertTrue(s.services[2].headless)
        XCTAssertEqual(s.services[2].ports, [])
    }

    func testReadyCountShownOnlyWhenKnownAndExpected() throws {
        let rows = try services().services
        XCTAssertEqual(rows[1].readyText, "2/3")
        XCTAssertNil(rows[2].readyText)
        XCTAssertNil(rows[3].readyText)
    }

    func testSearchesNameTypeAddressesPortsAndRoutes() throws {
        let rows = try services().services
        XCTAssertEqual(filterServices(rows, query: "SHOP.EXAMPLE").map(\.name), ["web"])
        XCTAssertEqual(filterServices(rows, query: "30443").map(\.name), ["web-public"])
        XCTAssertEqual(filterServices(rows, query: "externalname").map(\.name), ["legacy"])
        XCTAssertEqual(filterServices(rows, query: " ").count, 4)
    }
}

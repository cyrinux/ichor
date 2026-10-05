import XCTest
@testable import IchorCore

final class CertDetailsTests: XCTestCase {
    private let json = #"""
    {"conditions":[{"type":"Ready","status":"False","reason":"Failed","message":"order failed","time":1791194400000}],
     "requests":[{"name":"site-2","created":1791190800000,"conditions":[{"type":"Ready","status":"False","reason":"Pending"}],
       "orders":[{"name":"site-2-77","state":"pending","challenges":[
         {"name":"site-2-77-1","type":"HTTP-01","dnsName":"site.example.com","state":"pending","reason":"wrong status code '404'","presented":true},
         {"name":"site-2-77-2","type":"DNS-01","dnsName":"example.com","wildcard":true,"state":"invalid"}]}]}],
     "events":[{"time":1791195900000,"type":"Warning","reason":"PresentError","message":"404","object":"Challenge/site-2-77-1","count":5}],
     "log":["I2 propagation check failed"],"error":""}
    """#

    func testDecodesTheChain() throws {
        let d = try TalosJSON.decode(CertDetails.self, from: json)
        XCTAssertEqual(d.conditions.first?.health, .warning)
        let challenges = try XCTUnwrap(d.requests.first?.orders.first?.challenges)
        XCTAssertEqual(challenges.map(\.health), [.warning, .critical])
        XCTAssertEqual(challenges[1].domain, "*.example.com")
        XCTAssertEqual(d.events.first?.object, "Challenge/site-2-77-1")
        XCTAssertEqual(d.events.first?.count, 5)
        XCTAssertTrue(d.events.first?.warning ?? false)
        XCTAssertEqual(d.log, ["I2 propagation check failed"])
    }

    func testDefaults() throws {
        let d = try TalosJSON.decode(CertDetails.self, from: #"{"events":[{"reason":"Requested"}]}"#)
        XCTAssertEqual(d.requests, [])
        XCTAssertEqual(d.events.first?.count, 1)
        XCTAssertFalse(d.events.first?.warning ?? true)
    }
}

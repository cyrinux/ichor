import XCTest
@testable import IchorCore

final class IntegrationsTests: XCTestCase {
    private let json = #"""
    {"families":[
      {"id":"istio.io","groups":[
        {"name":"networking.istio.io","version":"v1","kinds":["Gateway","VirtualService"]},
        {"name":"security.istio.io","version":"v1","kinds":[]}]},
      {"id":"example.io","groups":[{"name":"broken.example.io","version":"v1"}]}],
     "supported":["argoproj.io","longhorn.io"]}
    """#

    func testDecodesReport() throws {
        let report = try TalosJSON.decode(IntegrationReport.self, from: json)
        XCTAssertEqual(report.families.map(\.id), ["istio.io", "example.io"])
        XCTAssertEqual(report.families[0].groups[0].kinds, ["Gateway", "VirtualService"])
        XCTAssertEqual(report.families[1].groups[0].kinds, [])
        XCTAssertEqual(report.supported, ["argoproj.io", "longhorn.io"])
    }

    func testOlderCoreWithoutFields() throws {
        let report = try TalosJSON.decode(IntegrationReport.self, from: "{}")
        XCTAssertTrue(report.families.isEmpty)
        XCTAssertTrue(report.supported.isEmpty)
    }

    func testPickingKeepsOnlyChosenGroups() throws {
        let family = try TalosJSON.decode(IntegrationReport.self, from: json).families[0]
        let picked = family.picking(["security.istio.io"])
        XCTAssertEqual(picked.id, "istio.io")
        XCTAssertEqual(picked.groups.map(\.name), ["security.istio.io"])
        XCTAssertEqual(picked.json, #"{"groups":[{"kinds":[],"name":"security.istio.io","version":"v1"}],"id":"istio.io"}"#)
    }
}

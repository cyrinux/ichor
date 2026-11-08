import Foundation
import XCTest

@testable import IchorCore

final class CloudOVHTests: XCTestCase {
    private func options() throws -> OVHOptions {
        let json = """
            {"project":"scratch","currency":"EUR","regions":["DE1","GRA11"],
             "flavors":[{"id":"a","name":"b3-8","region":"GRA11","vcpus":2,"ramGB":8,"diskGB":50,"hourly":"0.0512"},
                        {"id":"b","name":"b3-8","region":"DE1","vcpus":2,"ramGB":8,"diskGB":50,"hourly":""}],
             "images":[{"id":"t","name":"Talos","region":"GRA11","private":true,"talos":true},
                       {"id":"d","name":"Debian","region":"DE1","private":false,"talos":false}]}
            """
        return try JSONDecoder().decode(OVHOptions.self, from: Data(json.utf8))
    }

    func testARegionListsOnlyItsOwn() throws {
        let value = try options()
        XCTAssertEqual(value.flavors(in: "GRA11").map(\.id), ["a"])
        XCTAssertEqual(value.images(in: "GRA11").map(\.id), ["t"])
        XCTAssertFalse(value.images(in: "DE1").contains(where: \.talos))
        XCTAssertTrue(value.flavors(in: "elsewhere").isEmpty)
    }

    func testAFlavorWithoutACatalogHasNoPrice() throws {
        XCTAssertEqual(try options().flavors(in: "DE1").first?.hourly, "")
    }

    func testNullListsDecodeEmpty() throws {
        let json = #"{"regions":null,"flavors":null,"images":null}"#
        let value = try JSONDecoder().decode(OVHOptions.self, from: Data(json.utf8))
        XCTAssertTrue(value.regions.isEmpty && value.flavors.isEmpty && value.images.isEmpty)
    }

    func testARunWithAnInstanceHasTheCloudTimeline() {
        let spec = OVHCreateSpec(region: "GRA11", flavorId: "a", imageId: "t", hostname: "node-c")
        let plain = JoinRequest(
            address: "", template: "", role: "", hostname: "node-c", disk: "", staticAddress: "", ovh: spec)
        XCTAssertEqual(plain.phases, JoinPhase.cloudRun)
        let omni = JoinRequest(
            address: "", template: "", role: "", hostname: "node-c", disk: "", staticAddress: "", ovh: spec, omni: true)
        XCTAssertEqual(omni.phases, JoinPhase.cloudOmniRun)
    }
}

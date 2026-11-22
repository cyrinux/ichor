import Foundation
import XCTest

@testable import IchorCore

final class CloudScalewayTests: XCTestCase {
    // ScalewayOptions' shape (go/ichorgo/cloud_scaleway.go), with invented ids.
    private func options() throws -> ScalewayOptions {
        let json = """
            {"project":"00000000-0000-4000-8000-000000000001","zone":"fr-par-1","zones":["fr-par-1","nl-ams-1"],
             "types":[{"name":"DEV1-S","arch":"x86_64","vcpus":2,"ramGB":2,"localDiskGB":20,"hourly":"0.0099",
                       "currency":"EUR"},
                      {"name":"COPARM1-2C-8G","arch":"arm64","vcpus":2,"ramGB":8,"hourly":"0.0426",
                       "currency":"EUR","blockOnly":true},
                      {"name":"PLAY2-NANO","arch":"x86_64","vcpus":1,"ramGB":2,"hourly":""}],
             "images":[{"id":"img-ubuntu","name":"ubuntu","arch":"x86_64","sizeGB":10},
                       {"id":"img-talos","name":"talos v1.12.0 scaleway","arch":"x86_64","sizeGB":10,"talos":true}],
             "securityGroups":[{"id":"sg-web","name":"web"},
                               {"id":"sg-default","name":"Default security group","default":true}],
             "warnings":["Plain Talos: the node's security group must let the phone reach TCP 50000"]}
            """
        return try JSONDecoder().decode(ScalewayOptions.self, from: Data(json.utf8))
    }

    func testTalosImagesComeFirst() throws {
        XCTAssertEqual(try options().imagesTalosFirst.map(\.id), ["img-talos", "img-ubuntu"])
    }

    func testTypesFollowTheImageArchitecture() throws {
        let value = try options()
        let talos = try XCTUnwrap(value.images.first(where: \.talos))
        XCTAssertEqual(value.types(for: talos).map(\.name), ["DEV1-S", "PLAY2-NANO"])
        XCTAssertEqual(value.types(for: nil).count, 3)
        XCTAssertTrue(value.types[1].blockOnly)
    }

    func testTheDefaultSecurityGroupIsPreselected() throws {
        XCTAssertEqual(try options().defaultSecurityGroup?.id, "sg-default")
    }

    func testCostLine() throws {
        let value = try options()
        XCTAssertEqual(value.types[0].hourlyPrice, "0.0099 EUR")
        XCTAssertNil(value.types[2].hourlyPrice)
    }

    func testNullListsDecodeEmpty() throws {
        let json = #"{"zones":null,"types":null,"images":null,"securityGroups":null,"warnings":null}"#
        let value = try JSONDecoder().decode(ScalewayOptions.self, from: Data(json.utf8))
        XCTAssertTrue(value.zones.isEmpty && value.types.isEmpty && value.images.isEmpty)
        XCTAssertNil(value.defaultSecurityGroup)
    }

    func testAStoredKeyMovesToAnotherZone() throws {
        let stored = String(
            decoding: try JSONEncoder().encode(
                ScalewayCredential(accessKey: "SCWEXAMPLEKEY", secretKey: "s", project: "p", zone: "fr-par-1")),
            as: UTF8.self)
        let moved = ScalewayCredential.json(stored, inZone: "nl-ams-1")
        let key = try JSONDecoder().decode(ScalewayCredential.self, from: Data(moved.utf8))
        XCTAssertEqual(key.zone, "nl-ams-1")
        XCTAssertEqual(key.secretKey, "s")
        XCTAssertEqual(ScalewayCredential.json(stored, inZone: ""), stored)
        XCTAssertEqual(ScalewayCredential.json("", inZone: "nl-ams-1"), "")
    }

    func testARunWithAnInstanceHasTheCloudTimeline() {
        let spec = ScalewayCreateSpec(zone: "fr-par-1", type: "DEV1-S", imageId: "img-talos", hostname: "node-c")
        let plain = JoinRequest(
            address: "", template: "", role: "", hostname: "node-c", disk: "", staticAddress: "", scaleway: spec)
        XCTAssertTrue(plain.cloud)
        XCTAssertEqual(plain.phases, JoinPhase.cloudRun)
        let omni = JoinRequest(
            address: "", template: "", role: "", hostname: "node-c", disk: "", staticAddress: "", scaleway: spec,
            omni: true)
        XCTAssertEqual(omni.phases, JoinPhase.cloudOmniRun)
        XCTAssertEqual(omni.withReplacing(true).scaleway, spec)
    }
}

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

    func testTheManagedGroupIsPreselected() throws {
        let json = #"""
            {"securityGroups":[{"id":"sg-default","name":"Default security group","default":true},
                               {"id":"sg-nodes","name":"ichor-nodes","managed":true}],
             "groupWarning":"Plain Talos: check port 50000"}
            """#
        let value = try JSONDecoder().decode(ScalewayOptions.self, from: Data(json.utf8))
        XCTAssertEqual(value.defaultSecurityGroup?.id, "sg-nodes")
        XCTAssertEqual(value.groupWarning, "Plain Talos: check port 50000")
        XCTAssertEqual(try options().groupWarning, "")
    }

    // ScalewayNetworkPrepare's shape (go/ichorgo/cloud_scaleway_network.go), with documentation addresses.
    func testDecodesTheGuidedSecurityGroup() throws {
        let json = #"""
            {"securityGroupId":"sg-nodes","name":"ichor-nodes","created":false,"phoneIp":"203.0.113.7",
             "rules":[{"protocol":"all","port":0,"source":"198.51.100.10/32","description":"cp-1"},
                      {"protocol":"tcp","port":50000,"source":"203.0.113.7/32","description":"this phone"},
                      {"protocol":"tcp","port":50000,"source":"203.0.113.9/32"},
                      {"protocol":"udp","port":51820,"source":"192.0.2.0/24"}],
             "warnings":null}
            """#
        let network = try JSONDecoder().decode(ScalewayNetwork.self, from: Data(json.utf8))
        XCTAssertEqual(network.securityGroupId, "sg-nodes")
        XCTAssertTrue(network.warnings.isEmpty)
        let rules = network.rules
        XCTAssertTrue(rules[0].isMember && !rules[0].isPhone(network.phoneIp))
        XCTAssertTrue(rules[1].isPhone(network.phoneIp) && !rules[1].isMember)
        // An older address of the phone is a plain rule: listed, never removed.
        XCTAssertFalse(rules[2].isPhone(network.phoneIp) || rules[2].isMember)
        XCTAssertEqual(rules[3].plain, "udp/51820 ← 192.0.2.0/24")
        let spec = try JSONEncoder().encode(ScalewayNetworkSpec(zone: "nl-ams-1", role: "worker", omni: true))
        let decoded = try JSONSerialization.jsonObject(with: spec) as? [String: Any]
        XCTAssertEqual(decoded?["zone"] as? String, "nl-ams-1")
        XCTAssertEqual(decoded?["omni"] as? Bool, true)
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

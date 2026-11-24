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

    func testTheImageSpecUsesTheCoresNames() throws {
        let spec = ScalewayImageSpec(templateNode: "10.0.0.2", arch: "arm64", zone: "nl-ams-1")
        let object = try XCTUnwrap(
            JSONSerialization.jsonObject(with: JSONEncoder().encode(spec)) as? [String: Any])
        XCTAssertEqual(object["templateNode"] as? String, "10.0.0.2")
        XCTAssertEqual(object["arch"] as? String, "arm64")
        XCTAssertEqual(object["zone"] as? String, "nl-ams-1")
        XCTAssertEqual(object["type"] as? String, "")
        XCTAssertEqual(object["name"] as? String, "")
    }

    func testTheImageResultAndItsCostDecode() throws {
        let json = #"""
            {"imageId":"00000000-0000-4000-8000-0000000000aa","snapshotId":"s",
             "name":"talos-v1.12.0-scaleway-amd64-0123456789ab","arch":"amd64","talosVersion":"v1.12.0",
             "schematic":"0123","sizeGB":10}
            """#
        let result = try JSONDecoder().decode(ScalewayImageResult.self, from: Data(json.utf8))
        XCTAssertEqual(
            result.made,
            MadeImage(id: "00000000-0000-4000-8000-0000000000aa", name: "talos-v1.12.0-scaleway-amd64-0123456789ab"))
        let cost = try JSONDecoder().decode(
            ScalewayImageCost.self,
            from: Data(#"{"type":"DEV1-S","hourly":"0.0099","minutes":10,"currency":"EUR","snapshotGB":10}"#.utf8))
        XCTAssertEqual(cost.type, "DEV1-S")
        XCTAssertEqual(cost.snapshotGB, 10)
        XCTAssertEqual(cost.note, "")
    }

    func testTheImageArchitecturesAndTheTalosImageOfEach() throws {
        let value = try options()
        XCTAssertEqual(ScalewayArch.normalized("x86_64"), "amd64")
        XCTAssertEqual(ScalewayArch.normalized("aarch64"), "arm64")
        XCTAssertEqual(value.imageArches, ["amd64", "arm64"])
        XCTAssertTrue(value.hasTalosImage(arch: "amd64"))
        XCTAssertTrue(value.hasTalosImage(arch: "x86_64"))
        XCTAssertTrue(value.hasTalosImage(arch: ""))
        XCTAssertFalse(value.hasTalosImage(arch: "arm64"))
    }

    // Images the image run tagged (version=, schematic= of 12 characters), and the cluster's image.
    private func listed(template version: String, _ schematic: String, withRun: Bool = true) throws -> ScalewayOptions {
        let run = """
            {"id":"img-run","name":"run","arch":"x86_64","talos":true,"talosVersion":"v1.14.2",
             "schematic":"376567988ad3"},
            """
        let json = """
            {"types":[{"name":"DEV1-S","arch":"x86_64"},{"name":"COPARM1-2C-8G","arch":"arm64"}],
             "images":[{"id":"img-ubuntu","name":"ubuntu","arch":"x86_64"},
                       {"id":"img-old","name":"old","arch":"x86_64","talos":true,"talosVersion":"v1.13.5",
                        "schematic":"376567988ad3"},
                       \(withRun ? run : "")
                       {"id":"img-hand","name":"talos by hand","arch":"x86_64","talos":true}],
             "templateVersion":"\(version)","templateSchematic":"\(schematic)"}
            """
        return try JSONDecoder().decode(ScalewayOptions.self, from: Data(json.utf8))
    }

    func testTheClustersImageComesFirst() throws {
        let schematic = "376567988ad370138ad8b2698212367b8edcb69b5fd68c80be1f2ec7d603b4ba"
        let value = try listed(template: "v1.14.2", schematic)
        XCTAssertEqual(value.imagesTalosFirst.map(\.id), ["img-run", "img-old", "img-hand", "img-ubuntu"])
        XCTAssertEqual(value.images.filter { $0.matches(value) }.map(\.id), ["img-run"])
        XCTAssertTrue(value.hasClusterImage(arch: "amd64"))
        XCTAssertFalse(value.hasClusterImage(arch: "arm64"))

        // Only an older Talos: the make row comes first.
        let older = try listed(template: "v1.14.2", schematic, withRun: false)
        XCTAssertFalse(older.hasClusterImage(arch: "amd64"))
        XCTAssertTrue(older.hasTalosImage(arch: "amd64"))

        // Another schematic does not match either.
        let other = try listed(template: "v1.14.2", String(repeating: "0", count: 64))
        XCTAssertFalse(other.hasClusterImage(arch: "amd64"))

        // A template that cannot be read: any Talos image of the architecture counts, as before.
        let unread = try listed(template: "", "", withRun: false)
        XCTAssertTrue(unread.hasClusterImage(arch: "amd64"))
        XCTAssertEqual(unread.imagesTalosFirst.first?.id, "img-old")
    }

    func testAnImageRunHasItsOwnSteps() {
        let spec = ScalewayImageSpec(arch: "amd64", zone: "fr-par-1")
        let request = JoinRequest(
            address: "", template: "", role: "", hostname: "Talos amd64", disk: "", staticAddress: "",
            scalewayImage: spec)
        XCTAssertEqual(request.imageKind, .scalewayImage)
        XCTAssertEqual(request.phases, [.creating, .writing, .snapshot, .register, .cleanup, .done])
        XCTAssertTrue(request.imageRun)
        XCTAssertFalse(request.cloud)
        XCTAssertEqual(request.withReplacing(true).scalewayImage, spec)
    }
}

import Foundation
import XCTest

@testable import IchorCore

final class CloudAWSTests: XCTestCase {
    // The same fixture as app/src/test/java/name/levis/ichor/model/CloudAWSTest.kt.
    private func options() throws -> AWSOptions {
        let json = """
            {
              "region": "eu-west-1",
              "regions": ["eu-central-1", "eu-west-1"],
              "types": [
                {"name": "t4g.small", "vcpus": 2, "memoryGB": 2, "arch": "arm64", "burstable": true},
                {"name": "t3.small", "vcpus": 2, "memoryGB": 2, "arch": "amd64", "burstable": true},
                {"name": "m6i.large", "vcpus": 2, "memoryGB": 8, "arch": "amd64", "burstable": false}
              ],
              "subnets": [
                {"id": "subnet-a", "vpcId": "vpc-a", "vpcDefault": true, "cidr": "172.31.0.0/20",
                 "az": "eu-west-1a", "public": true, "name": ""},
                {"id": "subnet-b", "vpcId": "vpc-b", "vpcDefault": false, "cidr": "10.0.0.0/24",
                 "az": "eu-west-1b", "public": false, "name": "private"}
              ],
              "securityGroups": [
                {"id": "sg-a", "vpcId": "vpc-a", "name": "default", "description": "default VPC security group"},
                {"id": "sg-b", "vpcId": "vpc-b", "name": "nodes", "description": ""}
              ],
              "images": [
                {"id": "ami-custom", "arch": "amd64", "source": "custom", "name": "my talos",
                 "talosVersion": "", "schematic": ""},
                {"id": "ami-amd", "arch": "amd64", "source": "official", "name": "Talos v1.12.0 (amd64)",
                 "talosVersion": "v1.12.0", "schematic": ""},
                {"id": "ami-arm", "arch": "arm64", "source": "official", "name": "Talos v1.12.0 (arm64)",
                 "talosVersion": "v1.12.0", "schematic": ""}
              ],
              "warnings": ["a warning"]
            }
            """
        return try JSONDecoder().decode(AWSOptions.self, from: Data(json.utf8))
    }

    func testTheArchCouplesTypesAndImages() throws {
        let value = try options()
        XCTAssertEqual(value.arches, ["amd64", "arm64"])
        XCTAssertEqual(value.types(for: "amd64").map(\.name), ["t3.small", "m6i.large"])
        XCTAssertEqual(value.images(for: "amd64").map(\.id), ["ami-amd", "ami-custom"])
        XCTAssertEqual(value.images(for: "arm64").map(\.id), ["ami-arm"])
        XCTAssertEqual(value.images(for: "arm64").first?.official, true)
        XCTAssertEqual(value.types(for: "amd64").first?.memoryLabel, "2")
    }

    func testSecurityGroupsFollowTheSubnetsVPC() throws {
        let value = try options()
        XCTAssertEqual(value.securityGroups(in: "vpc-a").map(\.id), ["sg-a"])
        XCTAssertTrue(value.securityGroups(in: "vpc-z").isEmpty)
        XCTAssertEqual(value.subnets.first?.public, true)
        XCTAssertEqual(value.warnings, ["a warning"])
    }

    func testNullListsDecodeEmpty() throws {
        let json = #"{"region":"eu-west-1","types":null,"images":null,"warnings":null}"#
        let value = try JSONDecoder().decode(AWSOptions.self, from: Data(json.utf8))
        XCTAssertTrue(value.types.isEmpty && value.images.isEmpty && value.warnings.isEmpty)
    }

    func testTheCheckAndThePriceDecode() throws {
        let checkJSON = """
            {"account":"000000000000","arn":"arn","region":"eu-west-1","canCreate":false,"checked":false}
            """
        let check = try JSONDecoder().decode(AWSCheck.self, from: Data(checkJSON.utf8))
        XCTAssertFalse(check.checked)
        XCTAssertEqual(check.account, "000000000000")
        let priceJSON = #"{"hourly":"0.0208","currency":"USD"}"#
        let price = try JSONDecoder().decode(AWSPrice.self, from: Data(priceJSON.utf8))
        XCTAssertEqual(price.hourly, "0.0208")
        XCTAssertEqual(try JSONDecoder().decode(AWSPrice.self, from: Data(#"{"hourly":""}"#.utf8)).hourly, "")
    }

    func testTheCredentialKeepsItsKeysInAnotherRegion() throws {
        let keys = AWSCredential(accessKeyId: "AKIAEXAMPLE", secretAccessKey: "secret", region: "eu-west-1")
        let moved = keys.with(region: "us-east-1")
        XCTAssertEqual(moved.accessKeyId, "AKIAEXAMPLE")
        XCTAssertEqual(moved.region, "us-east-1")
        let decoded = try JSONDecoder().decode(AWSCredential.self, from: JSONEncoder().encode(moved))
        XCTAssertEqual(decoded, moved)
    }

    func testTheSpecUsesTheCoresNames() throws {
        let spec = AWSCreateSpec(
            region: "eu-west-1", instanceType: "t3.small", imageId: "ami-amd", subnetId: "subnet-a",
            securityGroupIds: ["sg-a"], associatePublicIp: true, hostname: "node-c")
        let json = String(decoding: try JSONEncoder().encode(spec), as: UTF8.self)
        let keys = ["\"instanceType\"", "\"imageId\"", "\"subnetId\"", "\"securityGroupIds\""]
        for key in keys + ["\"associatePublicIp\":true"] {
            XCTAssertTrue(json.contains(key), json)
        }
    }

    func testARunWithAnInstanceHasTheCloudTimeline() {
        let spec = AWSCreateSpec(
            region: "eu-west-1", instanceType: "t3.small", imageId: "ami-amd",
            subnetId: "subnet-a", hostname: "node-c")
        let plain = JoinRequest(
            address: "", template: "", role: "", hostname: "node-c", disk: "",
            staticAddress: "", aws: spec)
        XCTAssertEqual(plain.phases, JoinPhase.cloudRun)
        let omni = JoinRequest(
            address: "", template: "", role: "", hostname: "node-c", disk: "",
            staticAddress: "", aws: spec, omni: true)
        XCTAssertEqual(omni.phases, JoinPhase.cloudOmniRun)
        XCTAssertEqual(omni.withReplacing(true).aws, spec)
    }
}

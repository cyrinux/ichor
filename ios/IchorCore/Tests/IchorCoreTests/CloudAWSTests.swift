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

    // The cluster runs v1.12.0 with `schematic`: "own", "default", or that literal value.
    private func templated(_ schematic: String) throws -> AWSOptions {
        let fallback = "376567988ad370138ad8b2698212367b8edcb69b5fd68c80be1f2ec7d603b4ba"
        let own = "9d2c5e4b1a7f0836c4d2e1b9a0f7c6d5e4b3a2918f7e6d5c4b3a29180f7e6d5c"
        let json = """
            {
              "region": "eu-west-1",
              "images": [
                {"id": "ami-amd", "arch": "amd64", "source": "official", "name": "Talos v1.12.0 (amd64)",
                 "talosVersion": "v1.12.0", "schematic": "\(fallback)"},
                {"id": "ami-old", "arch": "amd64", "source": "custom", "name": "a old",
                 "talosVersion": "v1.11.5", "schematic": "\(own)"},
                {"id": "ami-own", "arch": "amd64", "source": "custom", "name": "z own",
                 "talosVersion": "v1.12.0", "schematic": "\(own)"},
                {"id": "ami-arm", "arch": "arm64", "source": "official", "name": "Talos v1.12.0 (arm64)",
                 "talosVersion": "v1.12.0", "schematic": "\(fallback)"}
              ],
              "templateVersion": "v1.12.0",
              "templateSchematic": "\(schematic == "own" ? own : schematic == "default" ? fallback : schematic)"
            }
            """
        return try JSONDecoder().decode(AWSOptions.self, from: Data(json.utf8))
    }

    func testTheClustersImageComesFirst() throws {
        let value = try templated("own")
        XCTAssertEqual(value.templateVersion, "v1.12.0")
        XCTAssertEqual(value.images(for: "amd64").map(\.id), ["ami-own", "ami-amd", "ami-old"])
        XCTAssertTrue(value.hasClusterImage(for: "amd64"))
        // An official AMI is the default schematic: not this cluster's image.
        XCTAssertFalse(value.hasClusterImage(for: "arm64"))

        // A cluster on the default schematic: the official AMI is its image.
        let plain = try templated("default")
        XCTAssertTrue(plain.images[0].matches(plain))
        XCTAssertTrue(plain.hasClusterImage(for: "arm64"))

        // Nothing to compare: no AMI matches, and an AMI of the account's own is enough.
        let unknown = try templated("")
        XCTAssertFalse(unknown.images.contains { $0.matches(unknown) })
        XCTAssertTrue(unknown.hasClusterImage(for: "amd64"))
        XCTAssertFalse(unknown.hasClusterImage(for: "arm64"))
        XCTAssertEqual(try options().templateVersion, "")
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

    // An invented Identity Center sign-in, as the core returns it (tokens included).
    private let signedIn = """
        {"region":"eu-west-1","ssoStartUrl":"https://example.awsapps.com/start","ssoRegion":"eu-west-1",\
        "accountId":"123456789012","accountName":"Sandbox","roleName":"IchorNodeCreator",\
        "ssoToken":{"accessToken":"a","refreshToken":"r","clientId":"c","clientSecret":"s"}}
        """

    func testAnIdentityCenterSignInIsReadWithoutItsTokens() throws {
        let credential = try JSONDecoder().decode(AWSCredential.self, from: Data(signedIn.utf8))
        XCTAssertTrue(credential.identityCenter)
        XCTAssertEqual(credential.role.label, "Sandbox · IchorNodeCreator")
        let keys = AWSCredential(accessKeyId: "AKIAEXAMPLE", secretAccessKey: "secret", region: "eu-west-1")
        XCTAssertFalse(keys.identityCenter)
    }

    func testMovingASignInToAnotherRegionKeepsItsTokens() throws {
        let moved = try XCTUnwrap(AWSCredential.moved(signedIn, to: "us-east-1"))
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(moved.utf8)) as? [String: Any])
        XCTAssertEqual(object["region"] as? String, "us-east-1")
        XCTAssertEqual((object["ssoToken"] as? [String: Any])?["refreshToken"] as? String, "r")
        XCTAssertNil(AWSCredential.moved("not json", to: "us-east-1"))
    }

    func testTheSignInStepsDecode() throws {
        let startJSON = """
            {"verificationUri":"https://device.sso/","verificationUriComplete":"","userCode":"WXYZ-1234",\
            "expiresIn":600,"credential":"{}"}
            """
        let start = try JSONDecoder().decode(AWSSignInStart.self, from: Data(startJSON.utf8))
        XCTAssertEqual(start.openURL, "https://device.sso/")
        XCTAssertEqual(start.userCode, "WXYZ-1234")
        let finishJSON = """
            {"credential":"{}","roles":[{"accountId":"123456789012","roleName":"IchorNodeCreator"}]}
            """
        let finish = try JSONDecoder().decode(AWSSignInFinish.self, from: Data(finishJSON.utf8))
        XCTAssertEqual(finish.roles.first?.roleName, "IchorNodeCreator")
        let checkJSON = #"{"account":"123456789012","credential":"{}"}"#
        let check = try JSONDecoder().decode(AWSCheck.self, from: Data(checkJSON.utf8))
        XCTAssertEqual(check.credential, "{}")
        XCTAssertEqual(try JSONDecoder().decode(AWSRenewed.self, from: Data("{}".utf8)).credential, "")
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

    func testTheAMISpecUsesTheCoresNames() throws {
        let spec = AWSImageSpec(
            templateNode: "10.0.0.2", arch: "arm64", region: "eu-west-1", subnetId: "subnet-a", securityGroupId: "sg-a")
        let json = String(decoding: try JSONEncoder().encode(spec), as: UTF8.self)
        let keys = ["\"templateNode\":\"10.0.0.2\"", "\"arch\":\"arm64\"", "\"subnetId\""]
        for key in keys + ["\"securityGroupId\":\"sg-a\""] {
            XCTAssertTrue(json.contains(key), json)
        }
    }

    func testTheAMIResultAndItsCostDecode() throws {
        let result = try JSONDecoder().decode(
            AWSImageResult.self,
            from: Data(
                #"{"imageId":"ami-0abc","snapshotId":"snap-0abc","name":"talos-v1.12.0-aws-amd64","arch":"amd64","sizeGB":8}"#
                    .utf8))
        XCTAssertEqual(result.imageId, "ami-0abc")
        XCTAssertEqual(result.sizeGB, 8)
        XCTAssertEqual(result.schematic, "")

        let cost = try JSONDecoder().decode(
            AWSImageCost.self,
            from: Data(
                #"{"instanceType":"t3.small","hourly":"0.0228","currency":"USD","snapshotMonthlyPerGB":"0.05","minutes":10}"#
                    .utf8))
        XCTAssertEqual(cost.minutes, 10)
        XCTAssertTrue(cost.priced)
        let unpriced = try JSONDecoder().decode(
            AWSImageCost.self, from: Data(#"{"instanceType":"t4g.small","minutes":10}"#.utf8))
        XCTAssertFalse(unpriced.priced)
    }

    func testAnAMIRunHasItsOwnSteps() {
        let spec = AWSImageSpec(arch: "amd64", region: "eu-west-1", subnetId: "subnet-a")
        let request = JoinRequest(
            address: "", template: "", role: "", hostname: "Talos amd64", disk: "", staticAddress: "", awsImage: spec)
        XCTAssertEqual(request.phases, JoinPhase.awsImageRun)
        XCTAssertTrue(request.imageRun)
        XCTAssertFalse(request.cloud)
        XCTAssertEqual(request.withReplacing(true).awsImage, spec)

        let steps = joinTimeline(
            [JoinProgress(phase: "creating"), JoinProgress(phase: "register")], phases: JoinPhase.awsImageRun)
        XCTAssertEqual(steps.map(\.phase), JoinPhase.awsImageRun)
        XCTAssertEqual(steps[2].state, .done)
        XCTAssertEqual(steps[3].state, .current)
    }
}

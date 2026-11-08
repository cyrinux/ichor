import Foundation
import XCTest

@testable import IchorCore

final class CloudAzureTests: XCTestCase {
    // The same fixture as app/src/test/java/name/levis/ichor/model/CloudAzureTest.kt.
    private func options() throws -> AzureOptions {
        let json = """
            {
              "subscription": "00000000-0000-0000-0000-000000000000",
              "location": "westeurope",
              "subscriptions": [{"id": "00000000-0000-0000-0000-000000000000", "name": "Scratch"}],
              "locations": [
                {"name": "northeurope", "displayName": "North Europe"},
                {"name": "westeurope", "displayName": "West Europe"}
              ],
              "resourceGroups": [
                {"name": "talos", "location": "westeurope"},
                {"name": "north", "location": "NorthEurope"}
              ],
              "sizes": [
                {"name": "Standard_A1_v2", "vcpus": 1, "memoryGB": 2, "arch": "amd64", "generations": ["V1"]},
                {"name": "Standard_B2s", "vcpus": 2, "memoryGB": 4, "arch": "amd64", "generations": ["V1", "V2"]},
                {"name": "Standard_D2ps_v5", "vcpus": 2, "memoryGB": 8, "arch": "arm64", "generations": ["V2"]},
                {"name": "Standard_E2ps_v5", "vcpus": 2, "memoryGB": 16, "arch": "arm64", "generations": ["V2"]}
              ],
              "networks": [
                {"id": "vnet-a", "name": "talos-vnet", "resourceGroup": "talos", "location": "westeurope",
                 "subnets": [
                   {"id": "vnet-a/subnets/nodes", "name": "nodes", "cidr": "10.0.0.0/24", "nsgId": "nsg-a",
                    "natGateway": false},
                   {"id": "vnet-a/subnets/egress", "name": "egress", "cidr": "10.0.1.0/24", "nsgId": "",
                    "natGateway": true}
                 ]}
              ],
              "securityGroups": [{"id": "nsg-a", "name": "talos-nodes", "resourceGroup": "talos", "wideOpen": true}],
              "images": [
                {"id": "gallery-arm", "name": "talos/talos-arm64/1.12.0", "resourceGroup": "talos", "source": "gallery",
                 "arch": "arm64", "generation": "V2", "talosVersion": "v1.12.0", "schematic": "sample-schematic"},
                {"id": "image-amd", "name": "talos-by-hand", "resourceGroup": "talos", "source": "managed",
                 "arch": "amd64", "generation": "V1", "talosVersion": "", "schematic": ""}
              ],
              "warnings": ["a warning"]
            }
            """
        return try JSONDecoder().decode(AzureOptions.self, from: Data(json.utf8))
    }

    func testTheImageChoosesTheSizesThatBootIt() throws {
        let value = try options()
        let gallery = value.images[0]
        let managed = value.images[1]
        XCTAssertEqual(value.sizes(for: gallery).map(\.name), ["Standard_D2ps_v5", "Standard_E2ps_v5"])
        XCTAssertEqual(value.sizes(for: managed).map(\.name), ["Standard_A1_v2", "Standard_B2s"])
        XCTAssertEqual(gallery.source, "gallery")
        XCTAssertEqual(value.sizes[1].label, "Standard_B2s · 2 CPU · 4 GB")
    }

    func testTheNetworkChoosesItsSubnets() throws {
        let value = try options()
        let subnets = value.subnets(in: "vnet-a")
        XCTAssertEqual(subnets.map(\.name), ["nodes", "egress"])
        XCTAssertTrue(subnets[1].natGateway)
        XCTAssertEqual(subnets[1].nsgId, "")
        XCTAssertTrue(value.subnets(in: "vnet-z").isEmpty)
        XCTAssertEqual(value.securityGroups.first?.wideOpen, true)
    }

    func testTheResourceGroupGivesTheLocation() throws {
        let value = try options()
        XCTAssertEqual(value.location(of: "north"), "northeurope")
        XCTAssertEqual(value.location(of: "unknown"), "westeurope")
        XCTAssertEqual(value.locationLabel("westeurope"), "West Europe")
        XCTAssertEqual(value.locationLabel("eastus"), "eastus")
        XCTAssertEqual(value.warnings, ["a warning"])
    }

    func testTheDiskSizeIsOneAzureTakes() {
        XCTAssertEqual(AzureDisk.gigabytes(" 20 "), 20)
        XCTAssertEqual(AzureDisk.gigabytes("4095"), 4095)
        XCTAssertNil(AzureDisk.gigabytes("4096"))
        XCTAssertNil(AzureDisk.gigabytes("5"))
        XCTAssertNil(AzureDisk.gigabytes(""))
    }

    func testNullListsDecodeEmpty() throws {
        let json = #"{"location":"westeurope","sizes":null,"images":null,"warnings":null}"#
        let value = try JSONDecoder().decode(AzureOptions.self, from: Data(json.utf8))
        XCTAssertTrue(value.sizes.isEmpty && value.images.isEmpty && value.warnings.isEmpty)
    }

    func testTheCheckAndThePriceDecode() throws {
        let checkJSON = """
            {"tenant":"t","subscription":"00000000-0000-0000-0000-000000000000","subscriptionName":"Scratch",
             "canCreate":false,"checked":false}
            """
        let check = try JSONDecoder().decode(AzureCheck.self, from: Data(checkJSON.utf8))
        XCTAssertFalse(check.checked)
        XCTAssertEqual(check.subscriptionName, "Scratch")
        let priceJSON = #"{"hourly":"0.0480","currency":"USD"}"#
        XCTAssertEqual(try JSONDecoder().decode(AzurePrice.self, from: Data(priceJSON.utf8)).hourly, "0.0480")
        XCTAssertEqual(try JSONDecoder().decode(AzurePrice.self, from: Data(#"{"hourly":""}"#.utf8)).hourly, "")
    }

    func testTheCredentialKeepsItsIdsInAnotherLocation() throws {
        let principal = AzureCredential(
            tenantId: "t", clientId: "c", clientSecret: "s", subscriptionId: "sub", environment: "AzureChinaCloud")
        let moved = principal.with(resourceGroup: "talos", location: "chinaeast2")
        XCTAssertEqual(moved.clientSecret, "s")
        XCTAssertEqual(moved.environment, "AzureChinaCloud")
        XCTAssertEqual(moved.resourceGroup, "talos")
        XCTAssertEqual(moved.with(location: "chinanorth3").resourceGroup, "talos")
        let decoded = try JSONDecoder().decode(AzureCredential.self, from: JSONEncoder().encode(moved))
        XCTAssertEqual(decoded, moved)
    }

    func testTheSpecUsesTheCoresNames() throws {
        let spec = AzureCreateSpec(
            resourceGroup: "talos", location: "westeurope", size: "Standard_B2s", imageId: "image-amd",
            subnetId: "vnet-a/subnets/nodes", securityGroupId: "nsg-a", associatePublicIp: true, osDiskGB: 20,
            hostname: "node-c")
        let json = String(decoding: try JSONEncoder().encode(spec), as: UTF8.self)
        let keys = ["\"resourceGroup\"", "\"size\"", "\"imageId\"", "\"subnetId\"", "\"securityGroupId\""]
        for key in keys + ["\"associatePublicIp\":true", "\"osDiskGB\":20"] {
            XCTAssertTrue(json.contains(key), json)
        }
    }

    func testARunWithAVMHasTheCloudTimeline() {
        let spec = AzureCreateSpec(
            resourceGroup: "talos", location: "westeurope", size: "Standard_B2s", imageId: "image-amd",
            subnetId: "s", hostname: "node-c")
        let plain = JoinRequest(
            address: "", template: "", role: "", hostname: "node-c", disk: "",
            staticAddress: "", azure: spec)
        XCTAssertTrue(plain.cloud)
        XCTAssertEqual(plain.phases, JoinPhase.cloudRun)
        let omni = JoinRequest(
            address: "", template: "", role: "", hostname: "node-c", disk: "",
            staticAddress: "", azure: spec, omni: true)
        XCTAssertEqual(omni.phases, JoinPhase.cloudOmniRun)
        XCTAssertEqual(omni.withReplacing(true).azure, spec)
    }

    func testTheImageSpecUsesTheCoresNames() throws {
        let spec = AzureImageSpec(
            templateNode: "10.0.0.2", arch: "arm64", resourceGroup: "talos", location: "westeurope",
            subnetId: "vnet-a/subnets/nodes", securityGroupId: "nsg-a")
        let object = try XCTUnwrap(
            JSONSerialization.jsonObject(with: JSONEncoder().encode(spec)) as? [String: Any])
        XCTAssertEqual(object["templateNode"] as? String, "10.0.0.2")
        XCTAssertEqual(object["arch"] as? String, "arm64")
        XCTAssertEqual(object["resourceGroup"] as? String, "talos")
        XCTAssertEqual(object["location"] as? String, "westeurope")
        XCTAssertEqual(object["subnetId"] as? String, "vnet-a/subnets/nodes")
        XCTAssertEqual(object["securityGroupId"] as? String, "nsg-a")
        XCTAssertEqual(object["size"] as? String, "")
        XCTAssertEqual(object["name"] as? String, "")
    }

    func testTheImageResultAndItsEstimateDecode() throws {
        let json = #"""
            {"imageId":"image-new","name":"talos-v1.12.0-azure-amd64-0123456789ab","arch":"amd64",
             "talosVersion":"v1.12.0","schematic":"0123","generation":"V1","sizeGB":8}
            """#
        let result = try JSONDecoder().decode(AzureImageResult.self, from: Data(json.utf8))
        XCTAssertEqual(result.imageId, "image-new")
        XCTAssertEqual(result.generation, "V1")
        XCTAssertEqual(result.sizeGB, 8)
        XCTAssertEqual(result.made, MadeImage(id: "image-new", name: "talos-v1.12.0-azure-amd64-0123456789ab"))

        let estimate = try JSONDecoder().decode(
            AzureImageEstimate.self,
            from: Data(
                #"{"size":"Standard_B2s","hourly":"0.0416","currency":"USD","imageNote":"n","minutes":10}"#.utf8))
        XCTAssertEqual(estimate.size, "Standard_B2s")
        XCTAssertEqual(estimate.hourly, "0.0416")
        XCTAssertEqual(estimate.minutes, 10)
        let unpriced = try JSONDecoder().decode(
            AzureImageEstimate.self, from: Data(#"{"size":"Standard_B2pls_v2","minutes":10}"#.utf8))
        XCTAssertEqual(unpriced.hourly, "")
    }

    func testAnImageRunHasItsOwnSteps() {
        let spec = AzureImageSpec(arch: "amd64", resourceGroup: "talos", location: "westeurope", subnetId: "s")
        let request = JoinRequest(
            address: "", template: "", role: "", hostname: "Talos amd64", disk: "", staticAddress: "",
            azureImage: spec)
        XCTAssertEqual(request.imageKind, .azureImage)
        XCTAssertEqual(request.phases, JoinPhase.imageRun)
        XCTAssertTrue(request.imageRun)
        XCTAssertFalse(request.cloud)
        XCTAssertEqual(request.withReplacing(true).azureImage, spec)

        let steps = joinTimeline(
            [JoinProgress(phase: "creating"), JoinProgress(phase: "snapshot")], phases: JoinPhase.imageRun)
        XCTAssertEqual(steps.map(\.phase), [.creating, .writing, .snapshot, .cleanup, .done])
        XCTAssertEqual(steps[1].state, .done)
        XCTAssertEqual(steps[2].state, .current)
        XCTAssertEqual(steps[3].state, .pending)
    }

    func testEachImageRunKnowsItsKind() {
        let plain = JoinRequest(address: "", template: "", role: "", hostname: "n", disk: "", staticAddress: "")
        XCTAssertNil(plain.imageKind)
        XCTAssertFalse(plain.imageRun)
        let snapshot = JoinRequest(
            address: "", template: "", role: "", hostname: "n", disk: "", staticAddress: "",
            snapshot: HetznerSnapshotSpec(arch: "amd64", location: "fsn1"))
        XCTAssertEqual(snapshot.imageKind, .hetznerSnapshot)
        XCTAssertEqual(snapshot.phases, JoinPhase.snapshotRun)
        let ami = JoinRequest(
            address: "", template: "", role: "", hostname: "n", disk: "", staticAddress: "",
            awsImage: AWSImageSpec(arch: "amd64", region: "eu-west-1", subnetId: "s"))
        XCTAssertEqual(ami.imageKind, .awsAMI)
        XCTAssertEqual(ami.phases, JoinPhase.awsImageRun)
    }

    // An invented Microsoft-account sign-in, as the core returns it (refresh token included).
    private let signedIn = """
        {"tenantId":"00000000-0000-0000-0000-000000000001","clientId":"04b07795-8ddb-461a-bbee-02f9e1bf7b46",
         "subscriptionId":"00000000-0000-0000-0000-000000000000","kind":"user",
         "refreshToken":"r","account":"someone@example.org"}
        """

    func testAMicrosoftAccountSignInIsReadAndEditedWithItsToken() throws {
        let credential = try JSONDecoder().decode(AzureCredential.self, from: Data(signedIn.utf8))
        XCTAssertTrue(credential.microsoftAccount)
        XCTAssertEqual(credential.account, "someone@example.org")
        let principal = AzureCredential(tenantId: "t", clientId: "c", clientSecret: "s", subscriptionId: "x")
        XCTAssertFalse(principal.microsoftAccount)

        let edited = try XCTUnwrap(AzureCredential.edited(signedIn, resourceGroup: "talos", location: "westeurope"))
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(edited.utf8)) as? [String: Any])
        XCTAssertEqual(object["refreshToken"] as? String, "r")
        XCTAssertEqual(object["resourceGroup"] as? String, "talos")
        XCTAssertEqual(object["location"] as? String, "westeurope")
        XCTAssertNil(AzureCredential.edited("not json", location: "westeurope"))

        // A renewal keeps what was picked meanwhile and takes only the new token.
        let renewed = try XCTUnwrap(AzureCredential.renewed(edited, from: #"{"refreshToken":"r2","location":"old"}"#))
        let merged = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(renewed.utf8)) as? [String: Any])
        XCTAssertEqual(merged["refreshToken"] as? String, "r2")
        XCTAssertEqual(merged["location"] as? String, "westeurope")
        XCTAssertNil(AzureCredential.renewed(edited, from: "{}"))
    }

    private func decode<T: Decodable>(_ type: T.Type, _ json: String) throws -> T {
        try JSONDecoder().decode(type, from: Data(json.utf8))
    }

    func testTheSignInStepsDecode() throws {
        let start = try decode(AzureSignInStart.self, #"{"userCode":"SAMPLE123","expiresIn":900,"credential":"{}"}"#)
        XCTAssertEqual(start.userCode, "SAMPLE123")
        let finish = try decode(
            AzureSignInFinish.self, #"{"credential":"{}","account":"a","subscriptions":[{"id":"sub-1","name":""}]}"#)
        XCTAssertEqual(finish.subscriptions.first?.label, "sub-1")
        let check = try decode(AzureCheck.self, #"{"subscription":"x","account":"a","credential":"{}"}"#)
        XCTAssertEqual(check.credential, "{}")
        XCTAssertEqual(try decode(AzureOptions.self, "{}").credential, "")
    }

    func testTheGuidedNetworkDecodes() throws {
        let network = try decode(
            AzureNetworkResult.self,
            #"{"networkId":"vnet","subnetId":"vnet/subnets/nodes","securityGroupId":"nsg","created":["nsg","vnet"]}"#)
        XCTAssertEqual(network.subnetId, "vnet/subnets/nodes")
        XCTAssertEqual(network.created, ["nsg", "vnet"])
        XCTAssertEqual(network.reused, [])
        let group = try decode(AzureSecurityGroup.self, #"{"id":"g","wideOpen":true,"wideOpenRule":"talos-api ← *"}"#)
        XCTAssertEqual(group.wideOpenRule, "talos-api ← *")
        let spec = AzureNetworkSpec(resourceGroup: "talos", location: "westeurope", omni: true)
        let encoded = String(decoding: try JSONEncoder().encode(spec), as: UTF8.self)
        XCTAssertTrue(encoded.contains(#""omni":true"#))
    }
}

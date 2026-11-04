import Foundation
import XCTest

@testable import IchorCore

final class CloudGCPTests: XCTestCase {
    // The same fixture as app/src/test/java/name/levis/ichor/model/CloudGCPTest.kt.
    private func options() throws -> GCPOptions {
        let json = """
            {
              "project": "example-project", "zone": "europe-west1-b",
              "projects": [{"id": "example-project", "name": "Example project"},
                           {"id": "other-project", "name": "other-project"}],
              "zones": [{"name": "europe-west1-b", "region": "europe-west1"},
                        {"name": "europe-west1-c", "region": "europe-west1"}],
              "machineTypes": [
                {"name": "e2-medium", "vcpus": 2, "memoryGB": 4, "arch": "amd64"},
                {"name": "t2a-standard-1", "vcpus": 1, "memoryGB": 4, "arch": "arm64"},
                {"name": "e2-standard-2", "vcpus": 2, "memoryGB": 8, "arch": "amd64"},
                {"name": "t2a-standard-2", "vcpus": 2, "memoryGB": 8, "arch": "arm64"}
              ],
              "networks": [
                {"name": "default", "selfLink": "https://example.test/networks/default", "autoSubnets": true,
                 "subnets": [{"name": "default", "selfLink": "https://example.test/subnetworks/default",
                              "region": "europe-west1", "cidr": "10.132.0.0/20"}]},
                {"name": "empty", "selfLink": "https://example.test/networks/empty", "autoSubnets": false,
                 "subnets": null}
              ],
              "images": [
                {"name": "talos-v1-12-0-amd64", "selfLink": "https://example.test/images/talos-amd64", "arch": "amd64",
                 "sizeGB": 10, "talosVersion": "v1.12.0", "schematic": "", "uefi": false},
                {"name": "talos-v1-12-0-arm64", "selfLink": "https://example.test/images/talos-arm64", "arch": "arm64",
                 "sizeGB": 10, "talosVersion": "v1.12.0", "schematic": "", "uefi": true},
                {"name": "my-own-arm", "selfLink": "https://example.test/images/my-own-arm", "arch": "arm64",
                 "sizeGB": 10, "talosVersion": "", "schematic": "", "uefi": false}
              ],
              "warnings": ["a warning",
                           "network default has no Cloud NAT in europe-west1: without a public address, the node needs a route out"]
            }
            """
        return try JSONDecoder().decode(GCPOptions.self, from: Data(json.utf8))
    }

    func testTheArchCouplesTypesAndImages() throws {
        let value = try options()
        XCTAssertEqual(value.arches, ["amd64", "arm64"])
        XCTAssertEqual(value.machineTypes(for: "amd64").map(\.name), ["e2-medium", "e2-standard-2"])
        let arm = try XCTUnwrap(value.machineTypes(for: "arm64").first)
        // An Arm VM boots UEFI images only: the unlabelled one without it is not offered.
        XCTAssertEqual(value.images(for: arm).map(\.name), ["talos-v1-12-0-arm64"])
        let amd = try XCTUnwrap(value.machineTypes(for: "amd64").first)
        XCTAssertEqual(value.images(for: amd).map(\.name), ["talos-v1-12-0-amd64"])
        XCTAssertEqual(amd.memoryLabel, "4")
    }

    func testOnlyNetworksWithASubnetHereArePicked() throws {
        let value = try options()
        XCTAssertEqual(value.usableNetworks.map(\.name), ["default"])
        XCTAssertEqual(value.subnets(in: "default", region: value.region).map(\.cidr), ["10.132.0.0/20"])
        XCTAssertTrue(value.subnets(in: "empty", region: value.region).isEmpty)
        XCTAssertEqual(value.region, "europe-west1")
        XCTAssertEqual(value.projects.first?.label, "Example project (example-project)")
        XCTAssertEqual(value.projects.last?.label, "other-project")
    }

    func testTheNATWarningGoesUnderTheSwitch() throws {
        let value = try options()
        XCTAssertEqual(value.otherWarnings, ["a warning"])
        XCTAssertEqual(value.natWarnings.count, 1)
    }

    func testNullListsDecodeEmpty() throws {
        let json = #"{"project":"p","zones":null,"machineTypes":null,"images":null,"warnings":null}"#
        let value = try JSONDecoder().decode(GCPOptions.self, from: Data(json.utf8))
        XCTAssertTrue(value.zones.isEmpty && value.machineTypes.isEmpty && value.images.isEmpty)
        XCTAssertTrue(value.warnings.isEmpty)
    }

    func testTheCheckAndThePriceDecode() throws {
        let checkJSON = """
            {"account":"node-creator@example-project.iam.gserviceaccount.com","project":"example-project",
             "canCreate":false,"checked":true,"missing":["compute.instances.create"]}
            """
        let check = try JSONDecoder().decode(GCPCheck.self, from: Data(checkJSON.utf8))
        XCTAssertTrue(check.checked)
        XCTAssertFalse(check.canCreate)
        XCTAssertEqual(check.missing, ["compute.instances.create"])
        let price = try JSONDecoder().decode(GCPPrice.self, from: Data(#"{"hourly":"0.0335","currency":"USD"}"#.utf8))
        XCTAssertEqual(price.hourly, "0.0335")
        XCTAssertEqual(try JSONDecoder().decode(GCPPrice.self, from: Data(#"{"hourly":""}"#.utf8)).hourly, "")
    }

    func testTheSpecUsesTheCoresNames() throws {
        let spec = GCPCreateSpec(
            project: "example-project", zone: "europe-west1-b", machineType: "e2-medium",
            image: "https://example.test/images/talos-amd64", subnetwork: "https://example.test/subnetworks/default",
            hostname: "node-c")
        let json = String(decoding: try JSONEncoder().encode(spec), as: UTF8.self)
        for key in ["\"machineType\"", "\"image\"", "\"subnetwork\"", "\"associatePublicIp\":true", "\"diskGB\":20"] {
            XCTAssertTrue(json.contains(key), json)
        }
        XCTAssertFalse(json.contains("serviceAccount"), json)
    }

    func testTheCredentialKeepsTheKeyInAnotherZone() throws {
        let credential = GCPCredential(serviceAccountJson: "{}", project: "example-project", zone: "europe-west1-b")
        let moved = credential.with(zone: "europe-west1-c")
        XCTAssertEqual(moved.serviceAccountJson, "{}")
        XCTAssertEqual(moved.project, "example-project")
        XCTAssertEqual(moved.zone, "europe-west1-c")
        let decoded = try JSONDecoder().decode(GCPCredential.self, from: JSONEncoder().encode(moved))
        XCTAssertEqual(decoded, moved)
    }

    func testTheKeyShowsItsEmailAndProjectOnly() {
        let key = """
            {"type":"service_account","project_id":"example-project",
             "private_key":"-----BEGIN PRIVATE KEY-----\\nAAA\\n-----END PRIVATE KEY-----\\n",
             "client_email":"node-creator@example-project.iam.gserviceaccount.com"}
            """
        let identity = GCPKeyIdentity(key: key)
        XCTAssertEqual(identity?.clientEmail, "node-creator@example-project.iam.gserviceaccount.com")
        XCTAssertEqual(identity?.projectId, "example-project")
        XCTAssertNil(GCPKeyIdentity(key: #"{"type":"authorized_user","client_email":"a@b.c","private_key":"k"}"#))
        XCTAssertNil(GCPKeyIdentity(key: "not json"))
    }

    func testTheHostNameFollowsComputeEngineNames() {
        XCTAssertTrue(GCPNames.valid("node-c"))
        XCTAssertFalse(GCPNames.valid("Node-C"))
        XCTAssertFalse(GCPNames.valid("1node"))
        XCTAssertFalse(GCPNames.valid("node-"))
        XCTAssertFalse(GCPNames.valid(String(repeating: "a", count: 64)))
    }

    func testTheActivationLinkIsFoundInTheCoresMessage() {
        let link = "https://console.developers.google.com/apis/api/compute.googleapis.com/overview?project=example-project"
        let message = "the Compute Engine API is not enabled in project example-project: enable it at " + link
        XCTAssertEqual(GCPNames.activationLink(in: message)?.absoluteString, link)
        XCTAssertNil(GCPNames.activationLink(in: "Google Cloud refused compute.instances.create"))
    }

    func testARunWithAVMHasTheCloudTimeline() {
        let spec = GCPCreateSpec(
            project: "example-project", zone: "europe-west1-b", machineType: "e2-medium", image: "img",
            subnetwork: "sub", hostname: "node-c")
        let plain = JoinRequest(
            address: "", template: "", role: "", hostname: "node-c", disk: "",
            staticAddress: "", gcp: spec)
        XCTAssertTrue(plain.cloud)
        XCTAssertEqual(plain.phases, JoinPhase.cloudRun)
        let omni = JoinRequest(
            address: "", template: "", role: "", hostname: "node-c", disk: "",
            staticAddress: "", gcp: spec, omni: true)
        XCTAssertEqual(omni.phases, JoinPhase.cloudOmniRun)
        XCTAssertEqual(omni.withReplacing(true).gcp, spec)
    }
}

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
        XCTAssertFalse(decoded.googleAccount)
        XCTAssertEqual(decoded.account, "")
    }

    // An invented sign-in, shaped as GCPSignInCredential's credential.
    private let signedIn = #"""
        {"googleSignIn":{"method":"gke","secrets":{"gcpGcloudSignIn":"1"},"session":{"refreshToken":"r1"}},
         "account":"someone@example.com","project":"","zone":""}
        """#

    func testTheSignInIsToldFromAKey() throws {
        let credential = try JSONDecoder().decode(GCPCredential.self, from: Data(signedIn.utf8))
        XCTAssertTrue(credential.googleAccount)
        XCTAssertEqual(credential.account, "someone@example.com")
        XCTAssertEqual(credential.serviceAccountJson, "")
        let key = try JSONDecoder().decode(
            GCPCredential.self, from: Data(#"{"serviceAccountJson":"{}","project":"p","googleSignIn":null}"#.utf8))
        XCTAssertFalse(key.googleAccount)
    }

    func testAProjectIsBoundAsJSONKeepingTheSignIn() throws {
        let bound = try XCTUnwrap(GCPCredential.withProject(signedIn, project: "example-project"))
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(bound.utf8)) as? [String: Any])
        XCTAssertEqual(object["project"] as? String, "example-project")
        XCTAssertEqual(object["account"] as? String, "someone@example.com")
        let signIn = try XCTUnwrap(object["googleSignIn"] as? [String: Any])
        XCTAssertEqual((signIn["session"] as? [String: String])?["refreshToken"], "r1")
        XCTAssertNil(GCPCredential.withProject("not json", project: "example-project"))

        let moved = try XCTUnwrap(GCPCredential.edited(bound, zone: "europe-west1-c"))
        let decoded = try JSONDecoder().decode(GCPCredential.self, from: Data(moved.utf8))
        XCTAssertEqual(decoded.project, "example-project")
        XCTAssertEqual(decoded.zone, "europe-west1-c")
        XCTAssertTrue(decoded.googleAccount)
    }

    func testARenewalTakesOnlyTheNewSignIn() throws {
        let stored = try XCTUnwrap(GCPCredential.edited(signedIn, project: "example-project", zone: "europe-west1-c"))
        let updated = #"""
            {"googleSignIn":{"method":"gke","secrets":{"gcpGcloudSignIn":"1"},"session":{"refreshToken":"r2"}},
             "account":"someone@example.com","project":"other-project","zone":"europe-west1-b"}
            """#
        let renewed = try XCTUnwrap(GCPCredential.renewed(stored, from: updated))
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(renewed.utf8)) as? [String: Any])
        let signIn = try XCTUnwrap(object["googleSignIn"] as? [String: Any])
        XCTAssertEqual((signIn["session"] as? [String: String])?["refreshToken"], "r2")
        XCTAssertEqual(object["project"] as? String, "example-project")
        XCTAssertEqual(object["zone"] as? String, "europe-west1-c")
        XCTAssertNil(GCPCredential.renewed(stored, from: "{}"))
        XCTAssertNil(GCPCredential.renewed("not json", from: updated))
    }

    func testTheSignInResultAndARenewedCheckDecode() throws {
        let json = #"""
            {"credential":"{}","account":"someone@example.com",
             "projects":[{"id":"example-project","name":"Example project"}],"more":true}
            """#
        let result = try JSONDecoder().decode(GCPSignInResult.self, from: Data(json.utf8))
        XCTAssertEqual(result.account, "someone@example.com")
        XCTAssertEqual(result.projects.map(\.id), ["example-project"])
        XCTAssertTrue(result.more)
        let empty = try JSONDecoder().decode(GCPSignInResult.self, from: Data(#"{"projects":null}"#.utf8))
        XCTAssertEqual(empty.projects, [])
        XCTAssertFalse(empty.more)

        let check = try JSONDecoder().decode(
            GCPCheck.self,
            from: Data(#"{"account":"someone@example.com","credential":"{}","renewHint":"sign in again"}"#.utf8))
        XCTAssertEqual(check.credential, "{}")
        XCTAssertEqual(check.renewHint, "sign in again")
        let options = try JSONDecoder().decode(GCPOptions.self, from: Data(#"{"credential":"{}"}"#.utf8))
        XCTAssertEqual(options.credential, "{}")
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
        let link =
            "https://console.developers.google.com/apis/api/compute.googleapis.com/overview"
            + "?project=example-project"
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

    func testTheImageSpecUsesTheCoresNames() throws {
        let spec = GCPImageSpec(
            templateNode: "10.0.0.2", arch: "arm64", project: "example-project", zone: "europe-west1-b",
            subnetwork: "https://example.test/subnetworks/default", associatePublicIp: false)
        let object = try XCTUnwrap(
            JSONSerialization.jsonObject(with: JSONEncoder().encode(spec)) as? [String: Any])
        XCTAssertEqual(object["templateNode"] as? String, "10.0.0.2")
        XCTAssertEqual(object["arch"] as? String, "arm64")
        XCTAssertEqual(object["project"] as? String, "example-project")
        XCTAssertEqual(object["zone"] as? String, "europe-west1-b")
        XCTAssertEqual(object["subnetwork"] as? String, "https://example.test/subnetworks/default")
        XCTAssertEqual(object["associatePublicIp"] as? Bool, false)
        XCTAssertEqual(object["machineType"] as? String, "")
        XCTAssertEqual(object["name"] as? String, "")
    }

    func testTheImageResultAndItsEstimateDecode() throws {
        let json = #"""
            {"image":"talos-v1-12-0-gcp-amd64-0123456789ab",
             "selfLink":"https://example.test/global/images/talos-v1-12-0-gcp-amd64-0123456789ab",
             "arch":"amd64","talosVersion":"v1.12.0","schematic":"0123","sizeGB":12}
            """#
        let result = try JSONDecoder().decode(GCPImageResult.self, from: Data(json.utf8))
        XCTAssertEqual(result.talosVersion, "v1.12.0")
        XCTAssertEqual(result.sizeGB, 12)
        XCTAssertEqual(
            result.made,
            MadeImage(
                id: "https://example.test/global/images/talos-v1-12-0-gcp-amd64-0123456789ab",
                name: "talos-v1-12-0-gcp-amd64-0123456789ab"))
        let unlinked = try JSONDecoder().decode(GCPImageResult.self, from: Data(#"{"image":"talos-a"}"#.utf8))
        XCTAssertEqual(unlinked.made.id, "talos-a")

        let estimate = try JSONDecoder().decode(
            GCPImageEstimate.self,
            from: Data(
                #"{"machineType":"e2-small","hourly":"0.0168","imageMonthlyPerGB":"0.0500","minutes":10}"#.utf8))
        XCTAssertEqual(estimate.machineType, "e2-small")
        XCTAssertEqual(estimate.hourly, "0.0168")
        XCTAssertEqual(estimate.imageMonthlyPerGB, "0.0500")
        XCTAssertEqual(estimate.diskMonthlyPerGB, "")
        XCTAssertEqual(estimate.minutes, 10)
    }

    func testAnImageRunHasItsOwnSteps() {
        let spec = GCPImageSpec(arch: "amd64", project: "example-project", zone: "europe-west1-b", subnetwork: "s")
        let request = JoinRequest(
            address: "", template: "", role: "", hostname: "Talos amd64", disk: "", staticAddress: "",
            gcpImage: spec)
        XCTAssertEqual(request.imageKind, .gcpImage)
        XCTAssertEqual(request.phases, JoinPhase.gcpImageRun)
        XCTAssertTrue(request.imageRun)
        XCTAssertFalse(request.cloud)
        XCTAssertEqual(request.withReplacing(true).gcpImage, spec)

        let steps = joinTimeline(
            [JoinProgress(phase: "creating"), JoinProgress(phase: "register")], phases: JoinPhase.gcpImageRun)
        XCTAssertEqual(steps.map(\.phase), [.creating, .writing, .register, .cleanup, .done])
        XCTAssertEqual(steps[1].state, .done)
        XCTAssertEqual(steps[2].state, .current)
        XCTAssertEqual(steps[3].state, .pending)
    }
}

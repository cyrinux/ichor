import Foundation
import XCTest

@testable import IchorCore

/// The Omni labels chosen on the card reach every create's specJSON as labelsJson.
final class CloudCreateLabelsTests: XCTestCase {
    private let labels = omniLabelsJSON(role: "controlplane", extra: "rack=b2")

    private func labelsField<T: Encodable>(_ spec: T) throws -> String? {
        let object = try JSONSerialization.jsonObject(with: try JSONEncoder().encode(spec)) as? [String: Any]
        return object?["labelsJson"] as? String
    }

    func testEveryCreateSpecCarriesTheLabels() throws {
        let fields = [
            try labelsField(
                HetznerCreateSpec(
                    serverType: "cx23", location: "fsn1", snapshotId: 2, hostname: "node-c", labelsJson: labels)),
            try labelsField(
                OVHCreateSpec(region: "GRA11", flavorId: "a", imageId: "t", hostname: "node-c", labelsJson: labels)),
            try labelsField(
                AWSCreateSpec(
                    region: "eu-west-1", instanceType: "t3.small", imageId: "ami-amd", subnetId: "subnet-a",
                    hostname: "node-c", labelsJson: labels)),
            try labelsField(
                AzureCreateSpec(
                    resourceGroup: "rg", location: "westeurope", size: "Standard_B2s", imageId: "img",
                    subnetId: "subnet", hostname: "node-c", labelsJson: labels)),
            try labelsField(
                GCPCreateSpec(
                    project: "example-project", zone: "europe-west1-b", machineType: "e2-medium", image: "image",
                    subnetwork: "subnet", hostname: "node-c", labelsJson: labels)),
        ]
        for field in fields {
            XCTAssertEqual(field, labels)
            let data = Data((field ?? "").utf8)
            let decoded = try JSONSerialization.jsonObject(with: data) as? [String: String]
            XCTAssertEqual(decoded, ["ichor/role": "controlplane", "rack": "b2"])
        }
    }

    func testPlainTalosSendsEmptyLabels() throws {
        let spec = HetznerCreateSpec(serverType: "cx23", location: "fsn1", snapshotId: 2, hostname: "node-c")
        XCTAssertEqual(try labelsField(spec), "")
    }
}

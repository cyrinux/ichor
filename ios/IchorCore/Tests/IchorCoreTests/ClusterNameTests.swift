import XCTest
@testable import IchorCore

final class ClusterNameTests: XCTestCase {
    private let prod = ContextSummary(name: "admin@talos-prod-eu-west-1", fingerprint: "fp-prod")
    private let lab = ContextSummary(name: "admin@lab", fingerprint: "fp-lab")

    func testGivenNameReplacesTheContextName() {
        let labels = ClusterLabels(names: ["fp-prod": "Prod"])
        XCTAssertEqual(labels.of(prod), "Prod")
        XCTAssertEqual(labels.of(lab), "admin@lab")
    }

    func testScreenshotModeShowsTheMaskedContextName() {
        let labels = ClusterLabels(names: ["fp-prod": "Prod"], masked: true)
        XCTAssertEqual(labels.of(prod), "admin@talos-prod-eu-west-1")
        XCTAssertNil(labels.given(prod))
    }

    func testNoFingerprintNoGivenName() {
        XCTAssertEqual(ClusterLabels(names: ["": "Old"]).of(ContextSummary(name: "admin@old")), "admin@old")
    }

    func testTypedNamesAreTrimmedAndBlankResets() {
        XCTAssertEqual(normalizeClusterName("  Prod "), "Prod")
        XCTAssertNil(normalizeClusterName("   "))
        XCTAssertEqual(normalizeClusterName(String(repeating: "x", count: 100))?.count, clusterNameMax)
    }

    func testNamesOfRemovedClustersAreForgotten() {
        let saved = ["fp-prod": "Prod", "fp-gone": "Gone"]
        XCTAssertEqual(keepClusterNames(saved: saved, fingerprints: ["fp-prod", "fp-lab"]), ["fp-prod": "Prod"])
    }
}

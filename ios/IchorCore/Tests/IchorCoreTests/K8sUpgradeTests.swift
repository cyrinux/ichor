import XCTest
@testable import IchorCore

final class K8sUpgradeTests: XCTestCase {
    func testPlanDecodes() throws {
        let plan = try TalosJSON.decode(K8sUpgradePlan.self, from: """
        {"from":"1.34.0","to":"1.35.1","supported":true,"supportedRange":"1.30–1.35",
         "steps":[{"kind":"controlplane","node":"10.0.0.1","hostname":"cp-1","component":"apiserver",
                   "image":"registry.k8s.io/kube-apiserver:v1.35.1","current":"registry.k8s.io/kube-apiserver:v1.34.0","changed":true},
                  {"kind":"kubelet","node":"10.0.0.3","component":"kubelet","image":"ghcr.io/siderolabs/kubelet:v1.35.1",
                   "current":"ghcr.io/siderolabs/kubelet:v1.35.1","changed":false}],
         "deprecatedApis":[{"api":"flowcontrol.apiserver.k8s.io/v1beta3","removedIn":"1.32","severity":"critical"}],
         "blockers":[],"warnings":null}
        """)
        XCTAssertEqual(plan.controlPlaneSteps.map(\.name), ["cp-1"])
        XCTAssertEqual(plan.kubeletSteps.map(\.name), ["10.0.0.3"])
        XCTAssertEqual(plan.changes, 1)
        XCTAssertTrue(plan.canStart)
        XCTAssertEqual(plan.steps[0].currentTag, "v1.34.0")
        XCTAssertEqual(plan.steps[0].newTag, "v1.35.1")
        XCTAssertEqual(plan.deprecatedAPIs.first?.severity, "critical")
    }

    func testVersionRange() {
        let choice = K8sVersionChoice(from: "1.34.2", supportedRange: "1.30–1.35", lo: 30, hi: 35)
        XCTAssertTrue(choice.allows("1.34.3"))
        XCTAssertTrue(choice.allows("1.35.0"))
        XCTAssertFalse(choice.allows("1.34.2"))
        XCTAssertFalse(choice.allows("1.33.9"))
        XCTAssertFalse(choice.allows("1.36.0"))
        XCTAssertFalse(K8sVersionChoice(from: "1.34.2", lo: 30, hi: 34).allows("1.35.0"))
        XCTAssertFalse(choice.allows("latest"))
    }
}

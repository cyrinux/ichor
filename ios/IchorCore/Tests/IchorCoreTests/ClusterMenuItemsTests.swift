import XCTest
@testable import IchorCore

final class ClusterMenuItemsTests: XCTestCase {
    private let share: [ClusterMenuItem] = [.incidentSummary, .shareLink]

    func testTalosAdminGetsEveryGroupInOrder() {
        let admin = ContextSummary(name: "admin@lab", roles: ["os:admin"])
        XCTAssertEqual(
            clusterMenuGroups(context: admin, canSwitch: true),
            [
                [.switchCluster, .manage],
                [.upgradeCluster, .upgradeKubernetes, .addNode, .updates, .runbooks],
                share,
            ])
    }

    func testTalosReaderCannotUpgrade() {
        let reader = ContextSummary(name: "reader@lab", roles: ["os:reader"])
        XCTAssertEqual(clusterMenuGroups(context: reader, canSwitch: true)[1], [.addNode, .updates, .runbooks])
    }

    func testOmniUpgradesAndAddsANode() {
        let omni = ContextSummary(name: "omni-lab", omni: true)
        XCTAssertEqual(
            clusterMenuGroups(context: omni, canSwitch: false),
            [
                [.manage],
                [.upgradeCluster, .upgradeKubernetes, .addNode, .updates, .runbooks],
                share,
            ])
    }

    func testKubeconfigClusterOperatesWithRunbooksOnly() {
        let kube = ContextSummary(name: "kind-dev", kind: ContextKind.kube)
        XCTAssertEqual(
            clusterMenuGroups(context: kube, canSwitch: true),
            [[.switchCluster, .manage], [.updates, .runbooks], share])
    }

    func testNoActiveClusterHasNoIncidentSummary() {
        XCTAssertEqual(clusterMenuGroups(context: nil, canSwitch: false), [[.manage], [.runbooks], [.shareLink]])
    }
}

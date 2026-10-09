import XCTest
@testable import IchorCore

final class KubeAccessTests: XCTestCase {
    private let json = """
    {"namespace":"shop","actions":{
      "restartWorkload":{"allowed":true},
      "scale":{"allowed":false,"verb":"patch","group":"apps","resource":"deployments/scale","namespace":"shop","reason":"no RBAC policy matched"},
      "execPod":{"allowed":true,"unknown":true},
      "deletePod":{"allowed":false,"verb":"delete","resource":"pods","namespace":"shop"},
      "drainNode":{"allowed":false,"verb":"create","resource":"pods/eviction"},
      "cordonNode":{"allowed":false,"unknown":true}
    }}
    """

    func testDecodesAndDenies() throws {
        let access = try TalosJSON.decode(KubeActionAccess.self, from: json)
        XCTAssertEqual(access.namespace, "shop")
        XCTAssertNil(access.denial(for: .restartWorkload))
        XCTAssertNil(access.denial(for: .execPod), "unknown never blocks")
        XCTAssertNil(access.denial(for: .cordonNode), "unknown never blocks, even when not allowed")
        XCTAssertNil(access.denial(for: .helmRollback), "not asked never blocks")

        let scale = try XCTUnwrap(access.denial(for: .scale))
        XCTAssertEqual(scale.verb, "patch")
        XCTAssertEqual(scale.deniedResource, "deployments.apps/scale")
        XCTAssertEqual(scale.namespace, "shop")
        XCTAssertFalse(scale.isClusterWide)
        XCTAssertEqual(scale.reason, "no RBAC policy matched")

        let delete = try XCTUnwrap(access.denial(for: .deletePod))
        XCTAssertEqual(delete.deniedResource, "pods")
        XCTAssertEqual(delete.group, "")
    }

    func testDenialFollowsTheObjectNamespace() throws {
        let access = try TalosJSON.decode(KubeActionAccess.self, from: json)
        XCTAssertNotNil(access.denial(for: .scale, in: "shop"))
        XCTAssertNil(access.denial(for: .scale, in: "other"), "asked for shop, says nothing about other")

        // Cluster-wide actions hold whatever namespace the object is in.
        let drain = try XCTUnwrap(access.denial(for: .drainNode, in: "other"))
        XCTAssertTrue(drain.isClusterWide)
        XCTAssertEqual(drain.deniedResource, "pods/eviction")

        // Every namespace asked, an object in one: a refusal everywhere may still allow it there.
        let everywhere = KubeActionAccess(namespace: "", actions: [
            "deletePod": KubeAccess(allowed: false, verb: "delete", resource: "pods"),
        ])
        XCTAssertNil(everywhere.denial(for: .deletePod, in: "shop"))
        XCTAssertNotNil(everywhere.denial(for: .deletePod, in: ""))
        XCTAssertNotNil(everywhere.denial(for: .deletePod))
    }

    func testFluxAccessAction() {
        XCTAssertEqual(FluxRef(kind: "Kustomization", namespace: "flux-system", name: "apps").accessAction, .fluxReconcile)
        XCTAssertEqual(FluxRef(kind: "HelmRelease", namespace: "flux-system", name: "web").accessAction, .fluxReconcileHelmRelease)
        XCTAssertEqual(FluxRef(kind: "GitRepository", namespace: "flux-system", name: "repo").accessAction, .fluxReconcileGitRepository)
        XCTAssertEqual(KubeAction.fluxReconcile(kind: "OCIRepository"), .fluxReconcileOCIRepository)
        XCTAssertEqual(KubeAction.fluxReconcile(kind: "HelmRepository"), .fluxReconcileHelmRepository)
        XCTAssertEqual(KubeAction.fluxReconcile(kind: "Bucket"), .fluxReconcileBucket)
        XCTAssertNil(FluxRef(kind: "ImagePolicy", namespace: "flux-system", name: "web").accessAction)
    }

    func testWorkloadActionsFollowTheKind() {
        XCTAssertEqual(KubeAction.restart(kind: "Deployment"), .restartWorkload)
        XCTAssertEqual(KubeAction.restart(kind: "StatefulSet"), .restartStatefulSet)
        XCTAssertEqual(KubeAction.restart(kind: "DaemonSet"), .restartDaemonSet)
        XCTAssertNil(KubeAction.restart(kind: "Rollout"))
        XCTAssertEqual(KubeAction.scale(kind: "Deployment"), .scale)
        XCTAssertEqual(KubeAction.scale(kind: "StatefulSet"), .scaleStatefulSet)
        XCTAssertNil(KubeAction.scale(kind: "DaemonSet"), "one pod per node: never scaled")
        XCTAssertEqual(kubeDistinctActions([.restartWorkload, nil, .restartDaemonSet, .restartWorkload]), [.restartWorkload, .restartDaemonSet])
    }

    func testBulkDenialAcrossNamespaces() {
        let refused = { (namespace: String) in
            KubeActionAccess(namespace: namespace, actions: [
                "argoSync": KubeAccess(allowed: false, verb: "patch", group: "argoproj.io", resource: "applications", namespace: namespace),
            ])
        }
        let access: [String: KubeActionAccess] = [
            "argocd": KubeActionAccess(namespace: "argocd", actions: ["argoSync": KubeAccess(allowed: true)]),
            "team-a": refused("team-a"),
            "team-b": refused("team-b"),
        ]
        XCTAssertNil(kubeBulkDenial(for: .argoSync, across: ["argocd", "argocd"], in: access))
        XCTAssertEqual(kubeBulkDenial(for: .argoSync, across: ["argocd", "team-a", "team-b"], in: access)?.namespace, "team-a",
                       "the first refused namespace gives the reason")
        XCTAssertNil(kubeBulkDenial(for: .argoSync, across: ["argocd", "elsewhere"], in: access), "not loaded never blocks")
        XCTAssertNil(kubeBulkDenial(for: nil, across: ["team-a"], in: access))
    }

    func testSharedNamespace() {
        XCTAssertEqual(kubeSharedNamespace(["shop", "shop"]), "shop")
        XCTAssertEqual(kubeSharedNamespace(["shop", "web"]), "")
        XCTAssertEqual(kubeSharedNamespace([]), "")
    }

    func testMissingFieldsOfferTheAction() throws {
        let access = try TalosJSON.decode(KubeActionAccess.self, from: #"{"actions":{"scale":{}}}"#)
        XCTAssertEqual(access.namespace, "")
        XCTAssertNil(access.denial(for: .scale))
        XCTAssertEqual(try TalosJSON.decode(KubeActionAccess.self, from: "{}").actions, [:])
    }

    func testEveryActionHasItsCoreName() {
        XCTAssertEqual(KubeAction.allCases.map(\.rawValue), [
            "restartWorkload", "restartStatefulSet", "restartDaemonSet", "scale", "scaleStatefulSet",
            "deletePod", "execPod", "debugPod", "suspendCronJob", "triggerCronJob", "helmRollback", "argoSync",
            "fluxReconcile", "fluxReconcileHelmRelease", "fluxReconcileGitRepository", "fluxReconcileOCIRepository",
            "fluxReconcileHelmRepository", "fluxReconcileBucket", "cordonNode", "drainNode",
        ])
        XCTAssertFalse(KubeAction.cordonNode.isNamespaced)
        XCTAssertFalse(KubeAction.drainNode.isNamespaced)
        XCTAssertTrue(KubeAction.argoSync.isNamespaced)
    }

    func testOneAnswerDecodes() throws {
        let can = try TalosJSON.decode(KubeAccess.self, from: #"{"allowed":false,"verb":"update","group":"","resource":"configmaps","namespace":"shop"}"#)
        XCTAssertTrue(can.isDenied)
        XCTAssertEqual(can.deniedResource, "configmaps")
        XCTAssertFalse(try TalosJSON.decode(KubeAccess.self, from: #"{"allowed":true}"#).isDenied)
    }

    func testWhoAmI() throws {
        let me = try TalosJSON.decode(KubeWhoAmI.self, from: #"{"user":"jane@example.com","groups":["dev","system:authenticated"]}"#)
        XCTAssertTrue(me.isKnown)
        XCTAssertEqual(me.user, "jane@example.com")
        XCTAssertEqual(me.groupsLine, "dev")

        XCTAssertFalse(try TalosJSON.decode(KubeWhoAmI.self, from: #"{"unknown":true}"#).isKnown)
        XCTAssertFalse(KubeWhoAmI(user: "").isKnown)
        XCTAssertEqual(KubeWhoAmI(user: "u", groups: ["system:authenticated"]).groupsLine, "")
    }
}

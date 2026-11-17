import XCTest
@testable import IchorCore

final class KubeTeleportTests: XCTestCase {
    func testDecodesTheProxysClusters() throws {
        let json =
            #"{"proxy":"proxy.example.org:443","teleportCluster":"teleport.example.org","user":"sample-user","#
            + #""clusters":[{"name":"sample-kube","context":"admin@test","signedIn":true,"expires":1700000000},"#
            + #"{"name":"other-kube","context":"teleport.example.org-other-kube"}]}"#
        let list = try KubeTeleportClusters.decode(json)
        XCTAssertEqual(list.clusters.map(\.name), ["sample-kube", "other-kube"])
        XCTAssertEqual(list.clusters[0].expires, 1_700_000_000)
        XCTAssertFalse(list.clusters[1].signedIn)
        XCTAssertTrue(list.signedInAnywhere)

        let demo = try KubeTeleportClusters.decode(#"{"clusters":[]}"#)
        XCTAssertTrue(demo.clusters.isEmpty)
        XCTAssertFalse(demo.signedInAnywhere)

        let added = try KubeTeleportAdded.decode(#"{"kubeconfig":"apiVersion: v1","context":"teleport.example.org-b"}"#)
        XCTAssertEqual(added.context, "teleport.example.org-b")
    }

    func testAnotherClusterOfTheProxyOnlyNeedsItsCertificate() {
        let values = ["kubeCluster": "other-kube", kubeTeleportUserKey: "sample-user"]
        let elsewhere = KubeSignInInfo(method: "teleport", kind: "browser", values: values)
        XCTAssertEqual(elsewhere.teleportUserElsewhere, "sample-user")
        XCTAssertEqual(elsewhere.teleportCertificateTarget, "other-kube")

        let signedIn = KubeSignInInfo(method: "teleport", kind: "browser", values: values, signedIn: true)
        XCTAssertNil(signedIn.teleportCertificateTarget)

        let nobody = KubeSignInInfo(method: "teleport", kind: "browser", values: ["kubeCluster": "other-kube"])
        XCTAssertNil(nobody.teleportCertificateTarget)
        XCTAssertNil(KubeSignInInfo(method: "oidc", kind: "browser", values: values).teleportUserElsewhere)
    }

    func testTheStoreKeepsTheProxysSharedSignIn() {
        let a = ContextSummary(
            name: "teleport.example.org-a", kind: ContextKind.kube, fingerprint: "k1", authKey: "teleport:proxy/a")
        let b = ContextSummary(
            name: "teleport.example.org-b", kind: ContextKind.kube, fingerprint: "k2", authKey: "teleport:proxy/a")
        XCTAssertEqual(authStoreFingerprints(talos: [], kube: [a, b]), ["k1", "teleport:proxy/a", "k2"])
    }
}

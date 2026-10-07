import XCTest
@testable import IchorCore

final class OmniTests: XCTestCase {
    func testSignInRequiredReason() {
        let message = "omni-sign-in-required: sign in to Omni: no key"
        XCTAssertEqual(omniSignInRequiredReason(message), "sign in to Omni: no key")
        XCTAssertTrue(isOmniSignInRequired(message))
        // After the prefix a wrapping call adds.
        XCTAssertEqual(omniSignInRequiredReason("10.0.0.1: omni-sign-in-required: sign in to Omni: the key expired"),
                       "sign in to Omni: the key expired")
        XCTAssertNil(omniSignInRequiredReason("kube-sign-in-required: sign in"))
        XCTAssertFalse(isOmniSignInRequired("connection refused"))
    }

    func testSignInInfoDecodes() throws {
        let info = try TalosJSON.decode(OmniSignInInfo.self, from: """
        {"signedIn":true,"method":"omni-browser","user":"ops@example.com","identity":"ops@example.com",
         "instance":"https://acme.omni.example.com","sessionExpires":1790000000}
        """)
        XCTAssertTrue(info.signedIn)
        XCTAssertFalse(info.isServiceAccount)
        XCTAssertTrue(info.canSignInWithBrowser)
        XCTAssertEqual(info.instance, "https://acme.omni.example.com")
        XCTAssertEqual(info.sessionExpires, 1_790_000_000)

        let none = try TalosJSON.decode(OmniSignInInfo.self, from: #"{"signedIn":false,"instance":"https://acme.omni.example.com"}"#)
        XCTAssertFalse(none.signedIn)
        XCTAssertNil(none.method)
        XCTAssertNil(none.user)
        XCTAssertFalse(none.canSignInWithBrowser)
        XCTAssertEqual(none.sessionExpires, 0)

        let account = try TalosJSON.decode(OmniSignInInfo.self, from: #"{"signedIn":true,"method":"omni-service-account","user":"ichor","instance":"x"}"#)
        XCTAssertTrue(account.isServiceAccount)
    }

    func testContextSummaryDecodesOmni() throws {
        let summary = try TalosJSON.decode(ConfigSummary.self, from: """
        {"current":"prod","contexts":[{"name":"prod","kind":"talos","fingerprint":"f1","endpoints":["https://acme.omni.example.com"],
         "nodes":null,"roles":[],"certNotAfter":0,"auth":"omni","omniCluster":"prod","identity":"ops@example.com"},
         {"name":"lab","fingerprint":"f2","roles":["os:reader"],"certNotAfter":1790000000}]}
        """)
        let omni = try XCTUnwrap(summary.context(named: "prod"))
        XCTAssertTrue(omni.isOmni)
        XCTAssertEqual(omni.omniCluster, "prod")
        XCTAssertEqual(omni.identity, "ops@example.com")
        XCTAssertEqual(omni.nodes, [])
        let lab = try XCTUnwrap(summary.context(named: "lab"))
        XCTAssertFalse(lab.isOmni)
        XCTAssertNil(lab.omniCluster)
        XCTAssertNil(lab.identity)
        // A kubeconfig context never is one, whatever its auth says.
        XCTAssertFalse(ContextSummary(name: "k", kind: ContextKind.kube, auth: "omni").isOmni)
    }

    func testOmniAllowsAllButIssueConfig() {
        let omni = ContextSummary(name: "prod", auth: ContextAuth.omni)
        XCTAssertTrue(omni.allows(.machineConfig))
        XCTAssertTrue(omni.allows(.workloads))
        XCTAssertTrue(omni.allows(.power))
        XCTAssertFalse(omni.allows(.issueConfig))
        // Without roles, a certificate context is allowed nothing.
        XCTAssertFalse(ContextSummary(name: "lab").allows(.resourceBrowser))
    }

    func testAuthStoreKeepsKubeAndOmniFingerprints() {
        let talos = [
            ContextSummary(name: "lab", fingerprint: "t1", roles: ["os:admin"]),
            ContextSummary(name: "prod", fingerprint: "t2", auth: ContextAuth.omni),
        ]
        let kube = [ContextSummary(name: "eks", kind: ContextKind.kube, fingerprint: "k1", auth: "eks")]
        XCTAssertEqual(authStoreFingerprints(talos: talos, kube: kube), ["k1", "t2"])
        XCTAssertEqual(authStoreFingerprints(talos: talos, kube: []), ["t2"])
        XCTAssertEqual(authStoreFingerprints(talos: [], kube: []), [])
    }
}

import XCTest
@testable import IchorCore

final class OmniTests: XCTestCase {
    func testDecodesAnOmniContext() throws {
        let json = """
        {"current":"acme","contexts":[{"name":"acme","kind":"talos","fingerprint":"abcdefghijklmnop","clusterId":"ponmlkjihgfedcba",
         "endpoints":["https://acme.eu-central-1.omni.example.com"],"nodes":null,"roles":[],"certNotAfter":0,
         "omni":true,"identity":"someone@example.com","cluster":"demo","signIn":"omni"}]}
        """
        let ctx = try XCTUnwrap(TalosJSON.decode(ConfigSummary.self, from: json).contexts.first)
        XCTAssertTrue(ctx.omni)
        XCTAssertEqual(ctx.identity, "someone@example.com")
        XCTAssertEqual(ctx.cluster, "demo")
        XCTAssertEqual(ctx.signIn, "omni")
        XCTAssertFalse(ctx.isKube)
    }

    func testCertificateContextIsNoOmni() throws {
        let json = """
        {"current":"lab","contexts":[{"name":"lab","endpoints":["10.0.0.1"],"roles":["os:admin"],"certNotAfter":1}]}
        """
        let ctx = try XCTUnwrap(TalosJSON.decode(ConfigSummary.self, from: json).contexts.first)
        XCTAssertFalse(ctx.omni)
        XCTAssertNil(ctx.identity)
    }

    func testOmniAllowsAllButIssuingCredentials() {
        let omni = ContextSummary(name: "acme", omni: true)
        XCTAssertTrue(omni.allows(.power))
        XCTAssertTrue(omni.allows(.upgrade))
        XCTAssertFalse(omni.allows(.issueConfig))
        XCTAssertFalse(omni.allows(.kubeconfig))
        XCTAssertFalse(omni.allows(.workloads))
        XCTAssertTrue(omni.allows(.workloads, kubeLinked: true))
    }

    func testServiceAccountKeyIsSecret() {
        XCTAssertEqual(kubeFieldInput("serviceAccountKey"), .secret)
    }
}

import XCTest
@testable import IchorCore

final class SealedSecretsTests: XCTestCase {
    private let json = #"""
    {"sealedSecrets":{"version":"v1alpha1","error":"","health":"critical","reasons":["keyCompromised"],
     "controller":{"namespace":"kube-system","name":"sealed-secrets-controller","ready":true,"replicas":1,"readyReplicas":1,"health":"ok","reasons":[]},
     "items":[
      {"namespace":"shop","name":"legacy-cert","keys":["tls.crt","tls.key"],"keyCount":2,"type":"kubernetes.io/tls","immutable":false,
       "scope":"strict","synced":false,"reason":"ErrUnsealFailed","message":"no key could decrypt secret","lastTransitionTime":1760000000000,
       "observedGeneration":1,"stale":true,"age":1759900000000,"health":"critical","reasons":["notSynced","stale"]},
      {"namespace":"shop","name":"taken","keys":["password"],"keyCount":1,"type":"Opaque","immutable":false,
       "scope":"strict","synced":false,"reason":"Unmanaged","message":"Resource already exists","lastTransitionTime":1760000000000,
       "observedGeneration":1,"stale":false,"age":1759900000000,"health":"warning","reasons":["unmanaged"]},
      {"namespace":"shop","name":"api-token","keys":["token"],"keyCount":1,"type":"Opaque","immutable":true,
       "scope":"namespace-wide","synced":true,"reason":"","message":"","lastTransitionTime":0,
       "observedGeneration":1,"stale":false,"age":1759900000000,"health":"ok","reasons":[]}],
     "truncated":false,"secretCount":3,
     "keys":[
      {"namespace":"kube-system","name":"sealed-secrets-key-new","state":"active","createdAt":1759400000000,"age":604800000},
      {"namespace":"kube-system","name":"sealed-secrets-key-old","state":"compromised","createdAt":1758000000000,"age":1814400000}]}}
    """#

    private let down = #"""
    {"sealedSecrets":{"version":"v1alpha1","error":"","health":"critical","reasons":["noKeys","controllerDown"],
     "controller":{"namespace":"kube-system","name":"sealed-secrets-controller","ready":false,"replicas":1,"readyReplicas":0,"health":"critical","reasons":["controllerDown"]},
     "items":[],"truncated":false,"secretCount":0,"keys":[]}}
    """#

    private var services: DataServices { get throws { try TalosJSON.decode(DataServices.self, from: json) } }

    func testDecodesControllerKeysAndItems() throws {
        let s = try services
        let sealed = try XCTUnwrap(s.sealedSecrets)
        XCTAssertEqual(sealed.version, "v1alpha1")
        XCTAssertEqual(sealed.health, .critical)
        XCTAssertEqual(sealed.reasons, [.keyCompromised])
        XCTAssertEqual(sealed.controller?.label, "kube-system/sealed-secrets-controller")
        XCTAssertEqual(sealed.keys.count, 2)
        XCTAssertTrue(sealed.keys[0].active)
        XCTAssertTrue(sealed.keys[1].compromised)
        XCTAssertEqual(sealed.secretCount, 3)
        let failed = sealed.items[0]
        XCTAssertEqual(failed.label, "shop/legacy-cert")
        XCTAssertFalse(failed.synced)
        XCTAssertEqual(failed.reasons, [.notSynced, .stale])
        XCTAssertEqual(failed.createdAt, 1759900000000)
        XCTAssertEqual(sealed.items[2].scope, "namespace-wide")
        XCTAssertTrue(sealed.items[2].immutable)
        XCTAssertEqual(s.detected, [.sealedSecrets])
    }

    func testSummaryCountsSecretsAndSectionProblems() throws {
        // 3 SealedSecrets: the failed and the unmanaged one need a look, plus the compromised key.
        XCTAssertEqual(try services.summary(.sealedSecrets), ServiceSummary(total: 3, attention: 3, health: .critical))
    }

    func testAlertsCoverFailedUnsealsOnly() throws {
        XCTAssertEqual(try dataIssuesOf(services), ["sealedsecrets|shop/legacy-cert": dataCritical])
    }

    func testAlertsOnControllerDownAndNoActiveKey() throws {
        let s = try TalosJSON.decode(DataServices.self, from: down)
        XCTAssertEqual(dataIssuesOf(s), [
            "sealedsecrets|kube-system/sealed-secrets-controller": dataCritical,
            "sealedsecrets|kube-system/sealed-secrets-key": dataCritical,
        ])
    }

    func testSystemTitle() {
        XCTAssertEqual(dataSystemTitle("sealedsecrets|shop/legacy-cert"), "Sealed Secrets")
    }
}

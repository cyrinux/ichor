import XCTest
@testable import IchorCore

final class KubeObjectSummaryTests: XCTestCase {
    // A KubeObjectSummary answer (go/ichorgo/kube_summary.go).
    private let json = #"""
    {"kind":"Pod","apiVersion":"v1","namespace":"shop","name":"web-1","health":"bad","healthReason":"Ready: ContainersNotReady",
     "phase":"Running","conditions":[{"type":"Ready","status":"False","reason":"ContainersNotReady","lastTransition":5,"tone":"bad"}],
     "owners":[{"via":"owner","group":"apps","version":"v1","resource":"replicasets","kind":"ReplicaSet","namespace":"shop",
                "name":"web","namespaced":true,"verbs":["get","update"],"controller":true},
               {"via":"flux","group":"kustomize.toolkit.fluxcd.io","kind":"Kustomization","namespace":"flux-system","name":"apps"},
               {"via":"helm","kind":"Release","namespace":"shop","name":"web"}],
     "labels":{"app":"web"},"annotations":{},"created":1,"deleting":0,"finalizers":[],
     "highlights":[{"key":"image","value":"web:1"}],
     "events":[{"type":"Warning","reason":"BackOff","message":"restarting","kind":"Pod","namespace":"shop","name":"web-1","count":2,"first":1,"last":2,"source":"kubelet"}],
     "eventsError":"","future":"ignored"}
    """#

    private func summary() throws -> KubeObjectSummary { try TalosJSON.decode(KubeObjectSummary.self, from: json) }

    func testDecodesTheGoSummary() throws {
        let s = try summary()
        XCTAssertEqual(s.healthTone, .bad)
        XCTAssertEqual(s.conditions.first?.toneValue, .bad)
        XCTAssertEqual(s.events.first?.reason, "BackOff")
        XCTAssertEqual(s.highlights.first?.value, "web:1")
        XCTAssertEqual(s.labels["app"], "web")
        XCTAssertEqual(s.owners.count, 3)
    }

    func testOwnersOpenOnlyWhenDiscoveryKnewThem() throws {
        let owners = try summary().owners
        let rs = try XCTUnwrap(owners[0].apiResource)
        XCTAssertEqual(rs.groupVersion, "apps/v1")
        XCTAssertEqual(rs.resource, "replicasets")
        XCTAssertTrue(rs.namespaced)
        XCTAssertTrue(rs.canUpdate)
        XCTAssertNil(owners[1].apiResource)
        XCTAssertTrue(owners[2].isHelmRelease)
        XCTAssertNil(owners[2].apiResource)
    }

    func testEmptyAnswerHasDefaults() throws {
        let s = try TalosJSON.decode(KubeObjectSummary.self, from: "{}")
        XCTAssertEqual(s.healthTone, .neutral)
        XCTAssertTrue(s.conditions.isEmpty && s.owners.isEmpty && s.events.isEmpty)
        XCTAssertEqual(summaryTone("warn"), .warn)
        XCTAssertEqual(summaryTone("purple"), .neutral)
    }
}

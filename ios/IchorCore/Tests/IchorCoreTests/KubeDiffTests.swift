import XCTest
@testable import IchorCore

final class KubeDiffTests: XCTestCase {
    // Shaped like KubeFluxDiff's answer (go/ichorgo/kube_flux_diff.go), already sorted by the Go core.
    private let json = #"""
    {"kind":"Kustomization","namespace":"flux-system","name":"apps","futureField":1,
     "revision":"main@sha1:4be1d0c9f2a7e3b18c6d5a0f9e8b7c6d5a4f3e2d","applied":"main@sha1:91c0a7e6d5b4c3a29180f7e6d5c4b3a291807f6e",
     "warnings":null,"resources":[
      {"group":"batch","version":"v1","kind":"Job","namespace":"web","name":"migrate","change":"error","diff":"","truncated":false,
       "error":"Kubernetes API (422 Invalid): spec.template: field is immutable"},
      {"group":"","version":"v1","kind":"ConfigMap","namespace":"web","name":"settings","change":"created",
       "diff":"--- live\n+++ wanted\n@@ -0,0 +1,2 @@\n+apiVersion: v1\n+kind: ConfigMap\n"},
      {"group":"apps","version":"v1","kind":"Deployment","namespace":"web","name":"web","change":"changed",
       "diff":"--- live\n+++ wanted\n@@ -40,3 +40,3 @@\n     spec:\n-      image: web:1\n+      image: web:2\n","truncated":true},
      {"group":"","version":"v1","kind":"Secret","namespace":"web","name":"creds","change":"encrypted"},
      {"group":"","version":"v1","kind":"Namespace","namespace":"","name":"web","change":"unchanged"},
      {"group":"","version":"v1","kind":"Service","namespace":"web","name":"web","change":"unchanged"}]}
    """#

    private var diff: FluxDiff { get throws { try TalosJSON.decode(FluxDiff.self, from: json) } }

    func testDecodesTheGoAnswer() throws {
        let d = try diff
        XCTAssertEqual(d.name, "apps")
        XCTAssertEqual(d.resources.count, 6)
        XCTAssertEqual(d.warnings, []) // null on the wire
        XCTAssertEqual(d.resources[0].change, .error)
        XCTAssertTrue(d.resources[0].error.contains("immutable"))
        XCTAssertTrue(d.resources[2].truncated)
        XCTAssertEqual(d.resources[2].id, "apps/Deployment/web/web")
    }

    func testCountsAndFolds() throws {
        let d = try diff
        XCTAssertEqual(d.counts.map(\.change), [.error, .created, .changed, .encrypted, .unchanged])
        XCTAssertEqual(d.counts.map(\.count), [1, 1, 1, 1, 2])
        XCTAssertEqual(d.changed.map(\.name), ["migrate", "settings", "web", "creds"])
        XCTAssertEqual(d.unchanged.map(\.kind), ["Namespace", "Service"])
        XCTAssertTrue(d.newRevision)
        XCTAssertFalse(d.inSync)
    }

    func testInSyncWhenNothingWouldBeWritten() {
        let quiet = FluxDiff(revision: "a", applied: "a", resources: [
            KubeDiffResource(kind: "Secret", name: "s", change: .encrypted),
            KubeDiffResource(kind: "ConfigMap", name: "c", change: .ignored),
            KubeDiffResource(kind: "Service", name: "w", change: .unchanged),
        ])
        XCTAssertTrue(quiet.inSync)
        XCTAssertFalse(quiet.newRevision)
        // An error is not "in sync": the reconcile would fail.
        let failing = FluxDiff(resources: quiet.resources + [KubeDiffResource(kind: "Job", name: "j", change: .error)])
        XCTAssertFalse(failing.inSync)
    }

    func testParsesDiffLines() throws {
        XCTAssertEqual(try diff.resources[2].lines, [
            DiffLine(.hunk, "@@ -40,3 +40,3 @@"),
            DiffLine(.context, "    spec:"),
            DiffLine(.removed, "      image: web:1"),
            DiffLine(.added, "      image: web:2"),
        ])
        XCTAssertEqual(parseDiffLines(""), [])
    }

    func testKeepsARemovedLineThatLooksLikeAHeader() {
        // A removed "-- note" line reads "--- note": only the first two lines are the header.
        let lines = parseDiffLines("--- live\n+++ wanted\n@@ -1,1 +0,0 @@\n--- note\n")
        XCTAssertEqual(lines.count, 2)
        XCTAssertEqual(lines.last, DiffLine(.removed, "-- note"))
    }

    func testUnknownChangeFallsBackToUnchanged() throws {
        let r = try TalosJSON.decode(KubeDiffResource.self, from: #"{"kind":"X","name":"x","change":"teleported"}"#)
        XCTAssertEqual(r.change, .unchanged)
    }
}

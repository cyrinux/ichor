import XCTest
@testable import IchorCore

final class CrossplaneTests: XCTestCase {
    // The shape of Go KubeCrossplane's demo answer, trimmed to one row per kind.
    private let json = #"""
    {"installed":true,"version":"v1","error":"",
     "packages":[
      {"group":"pkg.crossplane.io","version":"v1","resource":"providers","kind":"Provider","namespace":"","name":"provider-broken",
       "package":"registry.example.org/packages/provider-broken:v1","currentRevision":"provider-broken-revision",
       "installed":{"status":"False","reason":"PullFailed","message":"cannot pull package","lastTransition":1000},
       "healthy":{"status":"False","reason":"PullFailed","message":"","lastTransition":1000},
       "health":"critical","reasons":["providerUnhealthy"]}],
     "definitions":[
      {"group":"apiextensions.crossplane.io","version":"v1","resource":"compositeresourcedefinitions","kind":"CompositeResourceDefinition","namespace":"","name":"xdatabases.platform.example.org",
       "compositeGroup":"platform.example.org","compositeKind":"XDatabase","plural":"xdatabases","scope":"Namespaced",
       "versions":[{"name":"v1beta1","served":true,"referenceable":true}],
       "established":{"status":"False","reason":"ReconcileError","message":"waiting for the generated CRD","lastTransition":1000},
       "offered":{"status":"True","reason":"WatchingCompositeResourceClaim","message":"","lastTransition":1000},
       "compositions":1,"compositionNames":["database-standard"],"composites":0,"health":"warning","reasons":["notEstablished"]}],
     "composites":[
      {"group":"platform.example.org","version":"v1beta1","resource":"xbuckets","kind":"XBucket","namespace":"","name":"assets",
       "composition":"bucket-standard","resources":1,"claim":"assets-claim",
       "ready":{"status":"True","reason":"Available","message":"","lastTransition":1000},
       "synced":{"status":"True","reason":"ReconcileSuccess","message":"","lastTransition":1000},
       "lastTransition":1000,"health":"ok","reasons":[]}],
     "claims":[],
     "managed":[
      {"group":"storage.example.org","version":"v1beta1","resource":"buckets","kind":"Bucket","namespace":"","name":"resource-1",
       "providerConfig":"example-auth","externalName":"external-1","paused":false,
       "ready":{"status":"True","reason":"Available","message":"","lastTransition":1000},
       "synced":{"status":"False","reason":"ReconcileError","message":"AccessDenied: not authorized","lastTransition":1000},
       "lastTransition":1000,"health":"critical","reasons":["syncFailed"]},
      {"group":"sql.example.org","version":"v1beta1","resource":"databases","kind":"Database","namespace":"apps","name":"resource-5",
       "providerConfig":"example-auth","externalName":"external-5","paused":true,
       "ready":{"status":"False","reason":"Deleting","message":"","lastTransition":1000},
       "synced":{"status":"False","reason":"ReconcilePaused","message":"","lastTransition":1000},
       "lastTransition":1000,"health":"warning","reasons":["deleting","paused"]}],
     "managedTotal":2,"truncated":false,"counts":{"ok":1,"warning":2,"critical":2}}
    """#

    func testDecodesEveryKind() throws {
        let xp = try JSONDecoder().decode(Crossplane.self, from: Data(json.utf8))
        XCTAssertTrue(xp.installed)
        XCTAssertEqual(xp.packages.first?.reasons, [.providerUnhealthy])
        XCTAssertEqual(xp.definitions.first?.compositeKind, "XDatabase")
        let bucket = try XCTUnwrap(xp.managed.first)
        XCTAssertEqual(bucket.reasons, [.syncFailed])
        XCTAssertEqual(bucket.synced.message, "AccessDenied: not authorized")
        XCTAssertTrue(xp.managed[1].paused)
        XCTAssertEqual(xp.counts.critical, 2)
    }

    func testProblemsFirstAcrossKinds() throws {
        let xp = try JSONDecoder().decode(Crossplane.self, from: Data(json.utf8))
        // The failing Bucket (critical) before the deleting Database (warning); the ok XBucket is out.
        XCTAssertEqual(xp.problems.map(\.ref.name), ["resource-1", "resource-5"])
        XCTAssertEqual(xp.problems.first?.health, .critical)
    }

    func testFilterAndSearch() throws {
        let xp = try JSONDecoder().decode(Crossplane.self, from: Data(json.utf8))
        XCTAssertEqual(xp.managed.filtered(.critical, "").map(\.ref.name), ["resource-1"])
        XCTAssertEqual(xp.managed.filtered(.warning, "").map(\.ref.name), ["resource-5"])
        XCTAssertEqual(xp.managed.filtered(.all, "external-1").map(\.ref.name), ["resource-1"])
    }
}

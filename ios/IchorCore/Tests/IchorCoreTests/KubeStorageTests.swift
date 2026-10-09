import XCTest
@testable import IchorCore

final class KubeStorageTests: XCTestCase {
    private let json = #"""
    {"partialAccess":true,"claims":[
     {"namespace":"db","name":"data-pg-1","phase":"Bound","storageClass":"longhorn","provisioner":"driver.longhorn.io",
      "volume":"pv-pg","accessModes":["ReadWriteOnce"],"capacity":10737418240,"used":9663676416,"usedPercent":90,
      "measured":true,"pods":["pg-1"],"terminating":false,"managedBy":"cloudnative-pg","level":"warning"},
     {"namespace":"web","name":"uploads","phase":"Pending","level":"newer-level"}]}
    """#

    private func storage() throws -> KubeStorage {
        try JSONDecoder().decode(KubeStorage.self, from: Data(json.utf8))
    }

    func testDecodesTheCoreJSON() throws {
        let s = try storage()
        XCTAssertTrue(s.partialAccess)
        let pg = try XCTUnwrap(s.claims.first)
        XCTAssertEqual(pg.id, "db/data-pg-1")
        XCTAssertEqual(pg.usedFraction, 0.9, accuracy: 1e-9)
        XCTAssertEqual(pg.managedKind, .cnpg)
        XCTAssertEqual(pg.level, .warning)
        // An older or partial row decodes with its defaults; an unknown level reads as ok.
        XCTAssertEqual(s.claims[1].level, .ok)
        XCTAssertNil(s.claims[1].managedKind)
        XCTAssertEqual(s.claims[1].pods, [])
    }

    func testSearchesNameClassVolumeAndPods() throws {
        let claims = try storage().claims
        XCTAssertEqual(filterStorageClaims(claims, query: "PG-1").map(\.name), ["data-pg-1"])
        XCTAssertEqual(filterStorageClaims(claims, query: "longhorn").map(\.name), ["data-pg-1"])
        XCTAssertEqual(filterStorageClaims(claims, query: "web/").map(\.name), ["uploads"])
        XCTAssertEqual(filterStorageClaims(claims, query: " ").count, 2)
    }
}

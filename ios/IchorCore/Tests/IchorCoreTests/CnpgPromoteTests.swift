import XCTest
@testable import IchorCore

final class CnpgPromoteTests: XCTestCase {
    func testUnknownLagAndReplicaPicker() throws {
        let preview = try TalosJSON.decode(CnpgPromotePreview.self, from: """
        {"namespace":"shop","name":"pg","currentPrimary":"pg-1","instance":"pg-2","lagBytes":null,"lagError":"Forbidden",
        "ready":true,"canPromote":true,"warnings":["lagUnknown"]}
        """)
        XCTAssertNil(preview.lagBytes)
        XCTAssertTrue(preview.canPromote)
        let cluster = try TalosJSON.decode(CnpgCluster.self, from: """
        {"namespace":"shop","name":"pg","currentPrimary":"pg-1","targetPrimary":"pg-1",
        "instancePods":[{"name":"pg-1","ready":true},{"name":"pg-2","ready":true},{"name":"pg-3","ready":false}]}
        """)
        XCTAssertEqual(cluster.promotablePods.map(\.name), ["pg-2"])
        let restarting = try TalosJSON.decode(CnpgCluster.self, from: """
        {"namespace":"shop","name":"pg","currentPrimary":"pg-1","phase":"Primary instance is being restarted in-place",
        "instancePods":[{"name":"pg-2","ready":true}]}
        """)
        XCTAssertTrue(restarting.promotablePods.isEmpty)
    }
}

import XCTest
@testable import IchorCore

final class CnpgDestroyTests: XCTestCase {
    func testPreviewDecodesVolumesAndBlockers() throws {
        let preview = try TalosJSON.decode(
            CnpgDestroyPreview.self,
            from: """
                {"namespace":"shop","name":"pg","instance":"pg-2","currentPrimary":"pg-1","targetPrimary":"pg-1",
                "podPhase":"Pending","podNode":"","volumes":[{"name":"pg-2","role":"PG_DATA","node":"worker-gone","nodeState":"gone"}],
                "blockers":[],"warnings":[],"canDestroy":true}
                """)
        XCTAssertTrue(preview.canDestroy)
        XCTAssertEqual(preview.volumes.first?.nodeState, "gone")
        let refused = try TalosJSON.decode(CnpgDestroyPreview.self, from: #"{"blockers":["primary"]}"#)
        XCTAssertFalse(refused.canDestroy)
        XCTAssertEqual(refused.blockers, ["primary"])
    }

    func testRemovableInstancesSkipThePrimary() throws {
        let cluster = try TalosJSON.decode(
            CnpgCluster.self,
            from: """
                {"namespace":"shop","name":"pg","currentPrimary":"pg-1","targetPrimary":"pg-1",
                "instancePods":[{"name":"pg-3","phase":"Pending"},{"name":"pg-4","node":"worker-a","phase":"Running","ready":true}],
                "failedInstances":["pg-3","pg-1"],"missingInstances":["pg-1","pg-2"]}
                """)
        XCTAssertEqual(cluster.removableInstances, ["pg-2", "pg-3"])
        let plain = try TalosJSON.decode(CnpgCluster.self, from: #"{"namespace":"a","name":"b"}"#)
        XCTAssertTrue(plain.removableInstances.isEmpty)
    }
}

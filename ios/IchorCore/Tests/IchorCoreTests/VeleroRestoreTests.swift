import XCTest
@testable import IchorCore

final class VeleroRestoreTests: XCTestCase {
    func testDecodesPreviewWithDefaults() throws {
        let preview = try TalosJSON.decode(
            VeleroRestorePreview.self,
            from: #"""
                    {"namespace":"velero","backup":"apps-daily-2","phase":"Completed","completedAt":20,"expiresAt":99,
                     "storageLocation":"default","locationAvailable":true,"includedNamespaces":["demo"],
                     "includeClusterResources":"unset","snapshotVolumes":true,"totalItems":4,"itemsBackedUp":4,
                     "volumeSnapshots":2,"canRestore":true,"reasons":["partiallyFailed"],"restores":[
                      {"name":"apps-daily-2-r","phase":"InProgress","startedAt":30,"totalItems":4,"itemsRestored":1,
                       "namespaceMapping":{"demo":"demo-restored"},"includedNamespaces":["demo"],"existingResourcePolicy":"update"}]}
                """#)
        XCTAssertTrue(preview.canRestore)
        XCTAssertEqual(preview.snapshotVolumes, true)
        XCTAssertEqual(preview.includeClusterResources, "unset")
        XCTAssertEqual(preview.reasons, [.partiallyFailed])
        XCTAssertEqual(preview.restores.count, 1)
        let restore = try XCTUnwrap(preview.restores.first)
        XCTAssertTrue(restore.running)
        XCTAssertFalse(restore.finished)
        XCTAssertEqual(restore.namespaceMapping["demo"], "demo-restored")
        XCTAssertEqual(restore.existingResourcePolicy, "update")
    }

    func testMissingFieldsFallBackToDefaults() throws {
        let bare = try TalosJSON.decode(VeleroRestorePreview.self, from: "{}")
        XCTAssertFalse(bare.canRestore)
        XCTAssertTrue(bare.reasons.isEmpty)
        XCTAssertTrue(bare.restores.isEmpty)
        XCTAssertNil(bare.snapshotVolumes)
        XCTAssertEqual(bare.includeClusterResources, "unset")
    }

    func testRestorePhases() throws {
        let running = ["New", "InProgress", "WaitingForPluginOperations", "FinalizingPartiallyFailed"]
        let finished = ["Completed", "PartiallyFailed", "Failed", "FailedValidation"]
        for phase in running + finished {
            let r = try TalosJSON.decode(VeleroRestoreDetail.self, from: #"{"name":"r","phase":"\#(phase)"}"#)
            XCTAssertEqual(r.running, running.contains(phase), phase)
            XCTAssertEqual(r.finished, finished.contains(phase), phase)
        }
    }

    func testOptionsOmitEmptyAndUnset() throws {
        let bare = String(decoding: try JSONEncoder().encode(VeleroRestoreOptions()), as: UTF8.self)
        XCTAssertEqual(bare, #"{"existingResourcePolicy":"none"}"#)
        let full = String(
            decoding: try JSONEncoder().encode(
                VeleroRestoreOptions(
                    includedNamespaces: ["demo"], namespaceMapping: ["demo": "demo-restored"],
                    existingResourcePolicy: "update", restorePVs: false, includeClusterResources: nil)), as: UTF8.self)
        XCTAssertTrue(full.contains(#""includedNamespaces":["demo"]"#), full)
        XCTAssertTrue(full.contains(#""namespaceMapping":{"demo":"demo-restored"}"#), full)
        XCTAssertTrue(full.contains(#""restorePVs":false"#), full)
        XCTAssertFalse(full.contains("includeClusterResources"), full)
        XCTAssertFalse(full.contains("allNamespaces"), full)
        // Every namespace is asked for explicitly: the core refuses an empty pick otherwise.
        let all = String(decoding: try JSONEncoder().encode(VeleroRestoreOptions(allNamespaces: true)), as: UTF8.self)
        XCTAssertTrue(all.contains(#""allNamespaces":true"#), all)
        XCTAssertFalse(all.contains("includedNamespaces"), all)
    }
}

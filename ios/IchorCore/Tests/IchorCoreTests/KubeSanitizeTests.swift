import XCTest
@testable import IchorCore

final class KubeSanitizeTests: XCTestCase {
    private let preview = #"""
        {"pods":[
         {"namespace":"shop","name":"web-7d9f-abcde","status":"CrashLoopBackOff","ownerKind":"Deployment","ownerName":"web","ageSec":3600},
         {"namespace":"shop","name":"import-xyz12","status":"Completed","ownerKind":"Job","ownerName":"import","ageSec":7200},
         {"namespace":"shop","name":"export-abc34","status":"Completed","ageSec":86400},
         {"namespace":"shop","name":"queue-new","status":"Pending","ownerKind":"StatefulSet","ownerName":"queue","ageSec":30},
         {"namespace":"shop","name":"queue-old","status":"Pending","ageSec":600}],
         "truncated":true}
        """#

    private func decoded() throws -> KubeSanitizePreview {
        try TalosJSON.decode(KubeSanitizePreview.self, from: preview)
    }

    func testDecodesThePreview() throws {
        let p = try decoded()
        XCTAssertTrue(p.truncated)
        XCTAssertEqual(p.pods.count, 5)
        let web = p.pods[0]
        XCTAssertEqual(web.id, "shop/web-7d9f-abcde")
        XCTAssertEqual(web.ownerKind, "Deployment")
        XCTAssertTrue(web.hasOwner)
        XCTAssertFalse(p.pods[2].hasOwner)
        XCTAssertEqual(try TalosJSON.decode(KubeSanitizePreview.self, from: "{}"), KubeSanitizePreview(pods: []))
    }

    func testGroupsByStatusLargestFirst() throws {
        let groups = groupSanitizePods(try decoded().pods)
        XCTAssertEqual(groups.map(\.status), ["Completed", "Pending", "CrashLoopBackOff"])
        XCTAssertEqual(groups.map(\.pods.count), [2, 2, 1])
        XCTAssertEqual(groups[0].pods.map(\.name), ["export-abc34", "import-xyz12"])
        XCTAssertEqual(groupSanitizePods([]), [])
    }

    func testFreshPendingPodsAreUncheckedByDefault() throws {
        let pods = try decoded().pods
        let selected = sanitizeDefaultSelection(pods)
        XCTAssertFalse(selected.contains("shop/queue-new"))
        XCTAssertTrue(selected.contains("shop/queue-old"))
        XCTAssertEqual(selected.count, 4)
        // Exactly two minutes old: checked.
        XCTAssertTrue(sanitizeCheckedByDefault(SanitizePod(namespace: "a", name: "b", status: "Pending", ageSec: 120)))
        XCTAssertTrue(sanitizeCheckedByDefault(SanitizePod(namespace: "a", name: "b", status: "Error", ageSec: 1)))
    }

    func testNamesAreNewlineSeparatedInPreviewOrder() throws {
        let pods = try decoded().pods
        XCTAssertEqual(
            sanitizeNames(pods, selected: ["shop/queue-old", "shop/web-7d9f-abcde"]),
            "shop/web-7d9f-abcde\nshop/queue-old")
        XCTAssertEqual(sanitizeNames(pods, selected: []), "")
    }

    func testDecodesTheResult() throws {
        let r = try TalosJSON.decode(
            KubeSanitizeResult.self,
            from: #"{"deleted":3,"failed":[{"name":"shop/web-1","error":"not found"}]}"#)
        XCTAssertEqual(r.deleted, 3)
        XCTAssertEqual(r.failed.first?.name, "shop/web-1")
        XCTAssertEqual(r.failed.first?.error, "not found")
        XCTAssertEqual(try TalosJSON.decode(KubeSanitizeResult.self, from: #"{"deleted":0,"failed":null}"#).failed, [])
    }
}

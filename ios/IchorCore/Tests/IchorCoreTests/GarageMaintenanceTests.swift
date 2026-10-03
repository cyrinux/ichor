import XCTest
@testable import IchorCore

final class GarageMaintenanceTests: XCTestCase {
    // Shaped like KubeGarageBlockErrors' answer (the demo report).
    private let report = #"""
    {"errored":1294,"detailed":3,"live":1,"cleanupOnly":2,"staleRefs":1,"refcountMismatches":1,"retryable":1290,"repairsRunning":true,"futureField":1,
     "nodes":[
      {"id":"1b7e44c9a2f03d58","hostname":"garage-4kq7z","errored":648,"error":"","blocks":[
        {"hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","refcount":1,"errors":41,"lastTrySecs":300,"nextTrySecs":3300,
         "impact":"live","staleRef":false,"refcountMismatch":false,"error":"",
         "refs":[{"kind":"object","bucket":"4f1d0c2e9b7a6583","key":"photos/2024/beach.jpg","uploadId":"","version":"","live":true}]},
        {"hash":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","refcount":1,"errors":38,"impact":"stale-ref","staleRef":true,"refcountMismatch":true,
         "refs":[{"kind":"upload","bucket":"4f1d0c2e9b7a6583","key":"backups/db.tar","uploadId":"c3a9e1f07b2d4e68","live":false}]}]},
      {"id":"6d03b8f15e9a2c47","hostname":"garage-q8m2d","errored":646,"blocks":[{"hash":"cccc","impact":"cleanup","refs":null}]},
      {"id":"9c2f61a0d4e8b7c3","hostname":"","error":"Network error: Not connected","blocks":null}]}
    """#

    func testDecodesTheBlockReport() throws {
        let r = try TalosJSON.decode(GarageBlockReport.self, from: report)
        XCTAssertEqual(r.errored, 1294)
        XCTAssertEqual(r.detailed, 3)
        XCTAssertEqual(r.retryable, 1290)
        XCTAssertTrue(r.repairsRunning)
        XCTAssertEqual(r.verdict, .liveAffected)
        XCTAssertEqual(r.nodes.count, 3)

        let first = r.nodes[0]
        XCTAssertEqual(first.label, "garage-4kq7z")
        XCTAssertEqual(first.blocks[0].shortHash, "aaaaaaaaaaaa")
        XCTAssertEqual(first.blocks[0].impact, .live)
        XCTAssertEqual(first.blocks[0].nextTrySecs, 3300)
        XCTAssertEqual(first.blocks[0].refs[0].label, "4f1d0c2e9b7a6583/photos/2024/beach.jpg")
        XCTAssertTrue(first.blocks[0].refs[0].live)
        XCTAssertEqual(first.blocks[1].impact, .staleRef)
        XCTAssertTrue(first.blocks[1].staleRef)
        XCTAssertEqual(first.blocks[1].refs[0].uploadID, "c3a9e1f07b2d4e68")
        XCTAssertEqual(first.blocks[1].lastTrySecs, -1)

        XCTAssertEqual(r.nodes[1].blocks[0].impact, .cleanup)
        XCTAssertTrue(r.nodes[1].blocks[0].refs.isEmpty)
        XCTAssertEqual(r.nodes[2].label, "9c2f61a0d4e8b7c3")
        XCTAssertEqual(r.nodes[2].error, "Network error: Not connected")
        XCTAssertTrue(r.nodes[2].blocks.isEmpty)
    }

    func testVerdicts() throws {
        XCTAssertEqual(try TalosJSON.decode(GarageBlockReport.self, from: "{}").verdict, .clean)
        XCTAssertEqual(try TalosJSON.decode(GarageBlockReport.self, from: #"{"errored":4,"live":0}"#).verdict, .deletedOnly)
        XCTAssertEqual(try TalosJSON.decode(GarageBlockReport.self, from: #"{"errored":4,"live":2}"#).verdict, .liveAffected)
    }

    func testUnknownImpact() throws {
        let block = try TalosJSON.decode(GarageBlock.self, from: #"{"hash":"ab","impact":"weird"}"#)
        XCTAssertEqual(block.impact, .unknown)
    }

    func testRepairOutcomes() throws {
        let launched = try TalosJSON.decode(GarageRepairResult.self,
                                            from: #"{"blockRefs":true,"blockRc":true,"repairsRunning":false,"unreachable":false,"retried":1290,"errors":["node x: timeout"]}"#)
        XCTAssertEqual(launched.outcomes, [.launched(["block-refs", "block-rc"]), .retried(1290), .error("node x: timeout")])

        let blocked = try TalosJSON.decode(GarageRepairResult.self, from: #"{"repairsRunning":true,"unreachable":true,"errors":null}"#)
        XCTAssertEqual(blocked.outcomes, [.alreadyRunning, .unreachable])

        let nothing = try TalosJSON.decode(GarageRepairResult.self, from: #"{"errors":[]}"#)
        XCTAssertEqual(nothing.outcomes, [.nothingToDo])
    }
}

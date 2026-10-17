import Foundation
import XCTest
@testable import IchorCore

/// A Go error as gomobile hands it over: its message is the description.
private struct CoreFailure: LocalizedError {
    let message: String
    var errorDescription: String? { message }
}

/// Stands in for Go QRPart / QRPartsJoin: `ichor:<i>/<n>:<chunk>` parts, joined by
/// concatenating the chunks; a chunk "bad" fails the join like a corrupt gzip.
private enum FakeCore {
    static func part(_ text: String) throws -> String {
        guard text.hasPrefix("ichor:") else { return "" }
        let fields = text.dropFirst("ichor:".count).split(separator: ":", maxSplits: 1)
        let position = fields.first?.split(separator: "/") ?? []
        guard fields.count == 2, position.count == 2,
              let index = Int(position[0]), let total = Int(position[1])
        else { throw CoreFailure(message: "qr-parts-invalid: not a part") }
        return #"{"index":\#(index),"total":\#(total),"chunk":"\#(fields[1])"}"#
    }

    static func join(_ json: String) throws -> String {
        let parts = try JSONDecoder().decode([QRPart].self, from: Data(json.utf8))
        let chunks = parts.map(\.chunk)
        if chunks.contains("bad") { throw CoreFailure(message: "qr-parts-invalid: gzip: invalid header") }
        return "ichor-config:" + chunks.joined()
    }
}

final class QRPartsTests: XCTestCase {
    private func add(_ text: String, to collector: inout QRPartCollector) -> QRPartOutcome {
        collector.add(text, part: FakeCore.part, join: FakeCore.join)
    }

    func testThreePartsInReverseOrderJoinInIndexOrder() {
        var collector = QRPartCollector()
        XCTAssertEqual(add("ichor:3/3:cc", to: &collector), .added)
        XCTAssertEqual(collector.received, 1)
        XCTAssertEqual(collector.total, 3)
        XCTAssertEqual(collector.missing, [1, 2])
        XCTAssertEqual(add("ichor:2/3:bb", to: &collector), .added)
        XCTAssertEqual(collector.missing, [1])
        XCTAssertEqual(add("ichor:1/3:aa", to: &collector), .complete("ichor-config:aabbcc"))
        // Done: the collector is empty for the next import.
        XCTAssertFalse(collector.isCollecting)
        XCTAssertEqual(collector.received, 0)
    }

    func testExactDuplicateIsIgnored() {
        var collector = QRPartCollector()
        _ = add("ichor:1/2:aa", to: &collector)
        XCTAssertEqual(add("ichor:1/2:aa", to: &collector), .duplicate)
        XCTAssertEqual(collector.received, 1)
        XCTAssertEqual(collector.missing, [2])
    }

    func testOneCodeScanIsNotAPart() {
        var collector = QRPartCollector()
        XCTAssertEqual(add("ichor-config:H4sI", to: &collector), .notPart)
        XCTAssertEqual(add("context: home", to: &collector), .notPart)
        XCTAssertFalse(collector.isCollecting)
    }

    func testDifferentTotalRestartsFromTheNewPart() {
        var collector = QRPartCollector()
        _ = add("ichor:1/3:aa", to: &collector)
        _ = add("ichor:2/3:bb", to: &collector)
        XCTAssertEqual(add("ichor:2/4:xx", to: &collector), .restarted)
        XCTAssertEqual(collector.total, 4)
        XCTAssertEqual(collector.received, 1)
        XCTAssertEqual(collector.missing, [1, 3, 4])
    }

    func testOtherChunkForACollectedIndexRestarts() {
        var collector = QRPartCollector()
        _ = add("ichor:1/2:aa", to: &collector)
        XCTAssertEqual(add("ichor:1/2:zz", to: &collector), .restarted)
        XCTAssertEqual(collector.missing, [2])
        XCTAssertEqual(add("ichor:2/2:yy", to: &collector), .complete("ichor-config:zzyy"))
    }

    func testRestartThatCompletesAOnePartSet() {
        var collector = QRPartCollector()
        _ = add("ichor:1/2:aa", to: &collector)
        XCTAssertEqual(add("ichor:1/1:solo", to: &collector), .complete("ichor-config:solo"))
    }

    func testMalformedPartFailsWithoutLosingTheSet() {
        var collector = QRPartCollector()
        _ = add("ichor:1/2:aa", to: &collector)
        XCTAssertEqual(add("ichor:x", to: &collector), .failed(.invalid))
        XCTAssertEqual(collector.received, 1)
        XCTAssertEqual(collector.missing, [2])
    }

    func testCorruptJoinFailsAndEmptiesTheCollector() {
        var collector = QRPartCollector()
        _ = add("ichor:1/2:aa", to: &collector)
        XCTAssertEqual(add("ichor:2/2:bad", to: &collector), .failed(.invalid))
        XCTAssertFalse(collector.isCollecting)
    }

    func testJoinErrorCodesAreMapped() {
        for (message, expected) in [
            ("qr-parts-missing: 2 of 3", QRPartsError.missing),
            ("qr-parts-mismatch: totals differ", .mismatch),
            ("qr-parts-invalid: not a config", .invalid),
            ("something else", .invalid),
        ] {
            var collector = QRPartCollector()
            let outcome = collector.add(
                "ichor:1/1:aa",
                part: FakeCore.part,
                join: { _ in throw CoreFailure(message: message) }
            )
            XCTAssertEqual(outcome, .failed(expected), message)
        }
    }

    func testOutOfRangeOrUnreadablePartJSONIsInvalid() {
        for json in [#"{"index":0,"total":2,"chunk":"a"}"#, #"{"index":3,"total":2,"chunk":"a"}"#,
                     #"{"index":1,"total":0,"chunk":"a"}"#, "not json"] {
            var collector = QRPartCollector()
            let outcome = collector.add("ichor:1/2:a", part: { _ in json }, join: FakeCore.join)
            XCTAssertEqual(outcome, .failed(.invalid), json)
            XCTAssertFalse(collector.isCollecting)
        }
    }

    func testJoinReceivesEveryPartAsJSON() throws {
        var received = ""
        var collector = QRPartCollector()
        _ = collector.add("ichor:2/2:bb", part: FakeCore.part, join: { received = $0; return "" })
        _ = collector.add("ichor:1/2:aa", part: FakeCore.part, join: { received = $0; return "ok" })
        let parts = try JSONDecoder().decode([QRPart].self, from: Data(received.utf8))
        XCTAssertEqual(parts, [QRPart(index: 1, total: 2, chunk: "aa"), QRPart(index: 2, total: 2, chunk: "bb")])
    }

    func testResetEmptiesTheSet() {
        var collector = QRPartCollector()
        _ = add("ichor:1/3:aa", to: &collector)
        collector.reset()
        XCTAssertFalse(collector.isCollecting)
        XCTAssertEqual(collector.missing, [])
    }

    func testCoreMessagePrefixes() {
        XCTAssertEqual(QRPartsError(coreMessage: "qr-parts-missing: x"), .missing)
        XCTAssertNil(QRPartsError(coreMessage: "backup-unsupported"))
    }
}

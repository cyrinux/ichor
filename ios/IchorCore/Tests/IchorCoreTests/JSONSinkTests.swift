import XCTest
@testable import IchorCore

final class JSONSinkTests: XCTestCase {
    private struct Item: Decodable, Equatable {
        let n: Int
    }

    func testEmitsDecodedItemsAndDropsTheRest() {
        let items = Locked<[Item]>([])
        let sink = JSONSink<Item>(item: { item in items.withLock { $0.append(item) } }, done: { _ in })
        sink.emit(#"{"n":1}"#)
        sink.emit(nil)
        sink.emit("not json")
        sink.emit(#"{"n":2}"#)
        XCTAssertEqual(items.get(), [Item(n: 1), Item(n: 2)])
    }

    func testFinishReportsOnlyARealError() {
        let ends = Locked<[String?]>([])
        let sink = JSONSink<Item>(item: { _ in }, done: { end in ends.withLock { $0.append(end) } })
        sink.finish("")
        sink.finish(nil)
        sink.finish("boom")
        XCTAssertEqual(ends.get(), [nil, nil, "boom"])
    }
}

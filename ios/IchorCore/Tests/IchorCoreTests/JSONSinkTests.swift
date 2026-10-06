import XCTest
@testable import IchorCore

final class JSONSinkTests: XCTestCase {
    private struct Item: Decodable, Equatable {
        let n: Int
    }

    private final class Box<T>: @unchecked Sendable {
        var values: [T] = []
    }

    func testEmitsDecodedItemsAndDropsTheRest() {
        let items = Box<Item>()
        let sink = JSONSink<Item>(item: { items.values.append($0) }, done: { _ in })
        sink.emit(#"{"n":1}"#)
        sink.emit(nil)
        sink.emit("not json")
        sink.emit(#"{"n":2}"#)
        XCTAssertEqual(items.values, [Item(n: 1), Item(n: 2)])
    }

    func testFinishReportsOnlyARealError() {
        let ends = Box<String?>()
        let sink = JSONSink<Item>(item: { _ in }, done: { ends.values.append($0) })
        sink.finish("")
        sink.finish(nil)
        sink.finish("boom")
        XCTAssertEqual(ends.values, [nil, nil, "boom"])
    }
}

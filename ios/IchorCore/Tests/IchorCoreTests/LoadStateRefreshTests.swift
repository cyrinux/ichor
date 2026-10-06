import XCTest
@testable import IchorCore

final class LoadStateRefreshTests: XCTestCase {
    private func value(_ state: LoadState<Int>?) -> Int? {
        if case .loaded(let v, _, _)? = state { return v }
        return nil
    }

    func testSeedsOnlyWhileLoadingAndWhenSomethingIsKnown() {
        XCTAssertEqual(value(LoadState<Int>.loading.seeded(with: 7)), 7)
        XCTAssertNil(value(LoadState<Int>.loading.seeded(with: nil)))
        XCTAssertNil(value(LoadState<Int>.loaded(1, at: Date()).seeded(with: 7)))
        XCTAssertNil(value(LoadState<Int>.failed("x").seeded(with: 7)))
    }

    func testAReadForAnotherClusterIsDropped() {
        let shown = LoadState<Int>.loaded(1, at: Date())
        XCTAssertNil(value(shown.refreshed(with: .loaded(2, at: Date()), readFor: "a#1", current: "b#1")))
        XCTAssertEqual(value(shown.refreshed(with: .loaded(2, at: Date()), readFor: "a#1", current: "a#1")), 2)
    }

    func testAFailedRefreshKeepsTheDataAndNotesTheError() {
        let after = LoadState<Int>.loaded(1, at: Date()).refreshed(with: .failed("down"), readFor: "a", current: "a")
        guard case .loaded(let v, _, let error)? = after else { return XCTFail("data dropped") }
        XCTAssertEqual(v, 1)
        XCTAssertEqual(error, "down")
        guard case .failed(let message)? = LoadState<Int>.loading.refreshed(with: .failed("down"), readFor: "a", current: "a") else {
            return XCTFail("a screen with nothing to show gets the error page")
        }
        XCTAssertEqual(message, "down")
    }
}

import XCTest
@testable import IchorCore

final class ReadProgressTests: XCTestCase {
    func testDecodesGoJSON() throws {
        let json = """
            {"running":true,"done":5,"total":17,"steps":[{"id":"api","state":"done","ms":120},\
            {"id":"storage","state":"running","count":12,"total":40},{"id":"helm","state":"pending"},{"id":"x","state":"odd"}]}
            """
        let progress = try XCTUnwrap(ReadProgress.decode(json))
        XCTAssertTrue(progress.running)
        XCTAssertEqual(progress.fraction ?? 0, 5.0 / 17.0, accuracy: 0.0001)
        XCTAssertEqual(progress.steps[0], ReadStep(id: "api", state: .done, ms: 120))
        XCTAssertEqual(progress.steps[1], ReadStep(id: "storage", state: .running, count: 12, total: 40))
        XCTAssertEqual(progress.steps[2].state, .pending)
        // An unknown state reads as pending rather than failing the whole progress.
        XCTAssertEqual(progress.steps[3].state, .pending)
    }

    func testIdleProgressHasNoFraction() throws {
        let idle = try XCTUnwrap(ReadProgress.decode(#"{"running":false,"done":0,"total":0,"steps":[]}"#))
        XCTAssertFalse(idle.running)
        XCTAssertNil(idle.fraction)
        XCTAssertNil(ReadProgress.decode("not json"))
    }
}

import XCTest
@testable import IchorCore

final class KubeJobsTests: XCTestCase {
    private let json = #"""
    {"jobs":[
     {"namespace":"ops","name":"backup-manual-abcde","state":"failed","owner":"backup","manual":true,
      "started":1760000000000,"finished":1760000300000,"duration":300000,"completions":"0/1",
      "reason":"BackoffLimitExceeded","level":"critical"},
     {"namespace":"ops","name":"import","state":"suspended","completions":"0/1","level":"warning"},
     {"namespace":"ops","name":"migrate","state":"running","started":1760000000000,"duration":600000,"completions":"1/3","level":"newer-level"}]}
    """#

    private func jobs() throws -> [JobRow] {
        try JSONDecoder().decode(KubeJobs.self, from: Data(json.utf8)).jobs
    }

    func testDecodesTheCoreJSON() throws {
        let rows = try jobs()
        let failed = try XCTUnwrap(rows.first)
        XCTAssertEqual(failed.id, "ops/backup-manual-abcde")
        XCTAssertEqual(failed.level, .critical)
        XCTAssertEqual(failed.runState, .failed)
        XCTAssertEqual(failed.durationSeconds, 300)
        XCTAssertTrue(failed.manual)
        // A suspended Job has no run state of the CronJobs screen, nor a duration.
        XCTAssertTrue(rows[1].suspended)
        XCTAssertNil(rows[1].runState)
        XCTAssertNil(rows[1].durationSeconds)
        // An unknown level reads as ok.
        XCTAssertEqual(rows[2].level, .ok)
        XCTAssertEqual(rows[2].runState, .running)
    }

    func testSearchesNameOwnerStateAndReason() throws {
        let rows = try jobs()
        XCTAssertEqual(filterJobs(rows, query: "BACKOFF").map(\.name), ["backup-manual-abcde"])
        XCTAssertEqual(filterJobs(rows, query: "suspended").map(\.name), ["import"])
        XCTAssertEqual(filterJobs(rows, query: "ops/mig").map(\.name), ["migrate"])
        XCTAssertEqual(filterJobs(rows, query: " ").count, 3)
    }
}

import XCTest
@testable import IchorCore

final class WorkloadLogsTests: XCTestCase {
    private let sample = """
        {"pods":[{"name":"api-7d9f-x2k4","container":"app","color":1,"lines":1,"truncated":false},
                 {"name":"api-7d9f-b8q1","color":9}],
         "lines":[{"pod":"api-7d9f-x2k4","time":1000,"text":"request timeout","level":"error"},
                  {"pod":"api-7d9f-b8q1","text":"up"}],
         "matched":2,"scanned":3120,"truncated":true,"sinceMinutes":60}
        """

    func testDecoding() throws {
        let logs = try TalosJSON.decode(WorkloadLogs.self, from: sample)
        XCTAssertEqual(logs.pods.map(\.name), ["api-7d9f-x2k4", "api-7d9f-b8q1"])
        XCTAssertEqual(logs.matched, 2)
        XCTAssertEqual(logs.scanned, 3120)
        XCTAssertTrue(logs.truncated)
        XCTAssertEqual(logs.lines[0].level, "error")
        XCTAssertEqual(logs.lines[1].time, 0)
    }

    func testColorSlotStaysInThePalette() throws {
        let logs = try TalosJSON.decode(WorkloadLogs.self, from: sample)
        XCTAssertEqual(logs.colorSlot(of: "api-7d9f-x2k4"), 1)
        XCTAssertEqual(logs.colorSlot(of: "api-7d9f-b8q1"), 1)
        XCTAssertEqual(logs.colorSlot(of: "gone"), 0)
    }

    func testPodSuffix() {
        XCTAssertEqual(WorkloadLogLine(pod: "api-7d9f-x2k4").podSuffix, "x2k4")
        XCTAssertEqual(WorkloadLogLine(pod: "db-0").podSuffix, "0")
        XCTAssertEqual(WorkloadLogLine(pod: "solo").podSuffix, "solo")
    }

    func testAsText() throws {
        let logs = try TalosJSON.decode(WorkloadLogs.self, from: sample)
        XCTAssertEqual(
            logs.asText,
            "1970-01-01T00:00:01Z api-7d9f-x2k4 request timeout\n- api-7d9f-b8q1 up\n")
    }

    func testChoices() {
        XCTAssertEqual(logSinceChoices.last, 0)
        XCTAssertEqual(workloadLogTails.last, 2000)
        XCTAssertEqual(LogFilter(), LogFilter(text: "", regex: false, sinceMinutes: 0))
    }
}

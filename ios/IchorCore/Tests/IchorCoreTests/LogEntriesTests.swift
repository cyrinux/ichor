import XCTest
@testable import IchorCore

final class LogEntriesTests: XCTestCase {
    private let utc: Calendar = {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "UTC")!
        return calendar
    }()

    private func item(_ id: Int, ts: Int64 = 0, level: String = "info", source: String = "kubelet",
                      msg: String = "ok", fields: [LogField] = []) -> LogItem {
        LogItem(id: id, entry: LogEntry(ts: ts, level: level, source: source, msg: msg, fields: fields,
                                        raw: "\(level) \(source) \(msg) #\(id)"))
    }

    func testDecodeTailWithEntries() throws {
        let json = #"{"lines":["a","b"],"truncated":false,"entries":[{"ts":1800000000123,"level":"error","source":"kubelet","msg":"failed","fields":[{"k":"err","v":"boom"}],"raw":"a"},{"raw":"b"}]}"#
        let tail = try TalosJSON.decode(LogTail.self, from: json)
        let entries = tail.logEntries
        XCTAssertEqual(entries.count, 2)
        XCTAssertEqual(entries[0].logLevel, .error)
        XCTAssertEqual(entries[0].ts, 1_800_000_000_123)
        XCTAssertEqual(entries[0].fields, [LogField(k: "err", v: "boom")])
        XCTAssertTrue(entries[0].fields[0].isErrorKey)
        XCTAssertEqual(entries[1].logLevel, .none)
        XCTAssertEqual(entries[1].msg, "b")
        XCTAssertEqual(entries[1].ts, 0)
    }

    func testDecodeTailFallsBackToRawLines() throws {
        let missing = try TalosJSON.decode(LogTail.self, from: #"{"lines":["a","b"],"truncated":true}"#)
        XCTAssertNil(missing.entries)
        XCTAssertEqual(missing.logEntries, [LogEntry(raw: "a"), LogEntry(raw: "b")])

        let broken = try TalosJSON.decode(LogTail.self, from: #"{"lines":["a"],"truncated":false,"entries":"nope"}"#)
        XCTAssertEqual(broken.logEntries, [LogEntry(raw: "a")])

        let mismatched = try TalosJSON.decode(LogTail.self, from: #"{"lines":["a","b"],"truncated":false,"entries":[{"raw":"a"}]}"#)
        XCTAssertEqual(mismatched.logEntries.map(\.raw), ["a", "b"])
    }

    func testParseFollowedLine() {
        let parsed = parseLogEntry(json: #"{"ts":5,"level":"warn","source":"etcd","msg":"slow","fields":[],"raw":"line"}"#, line: "line")
        XCTAssertEqual(parsed.logLevel, .warn)
        XCTAssertEqual(parsed.source, "etcd")
        XCTAssertEqual(parseLogEntry(json: "", line: "plain"), LogEntry(raw: "plain"))
        // Go encodes an empty field list as null.
        XCTAssertEqual(parseLogEntry(json: #"{"ts":0,"level":"","source":"","msg":"m","fields":null,"raw":"m"}"#, line: "m").fields, [])
        XCTAssertEqual(parseLogEntry(json: #"{"msg":"m"}"#, line: "orig").raw, "orig")
        XCTAssertEqual(LogEntry(ts: 0, level: "fatal", msg: "x", raw: "x").logLevel, .none)
    }

    func testLevelFilterAndCounts() {
        let items = [item(0, level: "error"), item(1, level: "warn"), item(2), item(3, level: "debug"), item(4, level: "")]
        XCTAssertEqual(logLevelCounts(items), LogLevelCounts(all: 5, warnings: 2, errors: 1))
        XCTAssertEqual(logLevelCounts(items).count(.warnings), 2)
        XCTAssertEqual(items.filter { LogLevelFilter.warnings.matches($0.entry.logLevel) }.map(\.id), [0, 1])
        XCTAssertEqual(items.filter { LogLevelFilter.errors.matches($0.entry.logLevel) }.map(\.id), [0])
        XCTAssertEqual(items.filter { LogLevelFilter.all.matches($0.entry.logLevel) }.count, 5)
    }

    func testSearchMatchesRawCaseInsensitively() {
        let items = [item(0, msg: "Pulling image"), item(1, msg: "ready")]
        XCTAssertEqual(searchLogs(items, "PULLING").map(\.id), [0])
        XCTAssertEqual(searchLogs(items, "#1").map(\.id), [1])
        XCTAssertEqual(searchLogs(items, "").count, 2)
    }

    func testCollapseIdenticalConsecutiveEntries() {
        let items = [
            item(0, ts: 1_000), item(1, ts: 2_000), item(2, ts: 3_000),
            item(3, ts: 4_000, msg: "other"),
            item(4, ts: 5_000),
        ]
        let groups = collapseLogs(items)
        XCTAssertEqual(groups.map(\.count), [3, 1, 1])
        XCTAssertEqual(groups.map(\.id), [0, 3, 4])
        XCTAssertEqual(groups[0].firstTs, 1_000)
        XCTAssertEqual(groups[0].lastTs, 3_000)
        XCTAssertEqual(groups[0].entry.raw, items[2].entry.raw) // the newest occurrence
        XCTAssertEqual(collapseLogs([]), [])
    }

    func testCollapseTakesFirstKnownTime() {
        let groups = collapseLogs([item(0), item(1, ts: 2_000), item(2)])
        XCTAssertEqual(groups.map(\.count), [3])
        XCTAssertEqual(groups[0].firstTs, 2_000)
        XCTAssertEqual(groups[0].lastTs, 2_000)
    }

    func testCollapseComparesLevelSourceAndFields() {
        let items = [
            item(0), item(1, level: "warn"), item(2, level: "warn", source: "etcd"),
            item(3, level: "warn", source: "etcd", fields: [LogField(k: "a", v: "1")]),
            item(4, level: "warn", source: "etcd", fields: [LogField(k: "a", v: "1")]),
        ]
        XCTAssertEqual(collapseLogs(items).map(\.count), [1, 1, 1, 2])
    }

    func testCollapseKeepsIdWhileRunGrows() {
        let before = collapseLogs([item(7, ts: 1), item(8, ts: 2)])
        let after = collapseLogs([item(7, ts: 1), item(8, ts: 2), item(9, ts: 3)])
        XCTAssertEqual(before.map(\.id), [7])
        XCTAssertEqual(after.map(\.id), [7])
        XCTAssertEqual(after[0].count, 3)
    }

    func testDateDividers() {
        let day1 = Int64(1_800_000_000_000) // 2027-01-15 08:00 UTC
        let day2 = day1 + 86_400_000
        let groups = collapseLogs([item(0), item(1, ts: day1, msg: "a"), item(2, ts: day1 + 1, msg: "b"),
                                   item(3, msg: "undated"), item(4, ts: day2, msg: "c")])
        let rows = logRows(groups, calendar: utc)
        XCTAssertEqual(rows.map(\.id), [0, -2, 1, 2, 3, -5, 4])
        guard case .day(let first, _) = rows[1], case .day(let second, _) = rows[5] else {
            return XCTFail("expected dividers")
        }
        XCTAssertEqual(first, utc.startOfDay(for: logDate(day1)))
        XCTAssertEqual(second.timeIntervalSince(first), 86_400)
        XCTAssertEqual(Set(rows.map(\.id)).count, rows.count)
    }

    func testPipelineFiltersBeforeCollapsing() {
        // Hiding the info line makes the two errors consecutive.
        let items = [item(0, ts: 1, level: "error", msg: "x"), item(1, ts: 2), item(2, ts: 3, level: "error", msg: "x")]
        let all = logRows(items, search: "", level: .all, calendar: utc)
        XCTAssertEqual(all.filter { if case .group = $0 { true } else { false } }.count, 3)
        let errors = logRows(items, search: "", level: .errors, calendar: utc)
        guard case .group(let group) = errors.last else { return XCTFail("expected a group") }
        XCTAssertEqual(group.count, 2)
        XCTAssertEqual(errors.count, 2) // divider + group
    }

    func testTimeOfDay() {
        XCTAssertEqual(logTimeOfDay(1_800_000_000_123, calendar: utc), "08:00:00.123")
        XCTAssertEqual(logTimeOfDay(5, calendar: utc), "00:00:00.005")
    }
}

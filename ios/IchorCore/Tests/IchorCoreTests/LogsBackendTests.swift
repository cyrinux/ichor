import Foundation
import XCTest

@testable import IchorCore

final class LogsBackendTests: XCTestCase {
    func testDecodesTheGoAnswers() throws {
        let found = try JSONDecoder().decode(
            LogsDiscovery.self,
            from: Data(
                #"{"candidates":[{"kind":"loki","source":{"mode":"proxy","kind":"loki","namespace":"monitoring","service":"loki-gateway","port":80}}]}"#
                    .utf8))
        XCTAssertEqual(found.candidates.first?.source.label, "monitoring/loki-gateway:80")

        let result = try JSONDecoder().decode(
            LogsResult.self,
            from: Data(
                #"{"lines":[{"time":3000,"labels":{"pod":"a"},"text":"new"},{"time":1000,"labels":{},"text":"old"}],"stats":{"returned":2,"truncated":true}}"#
                    .utf8))
        XCTAssertEqual(result.lines.first?.pod, "a")
        XCTAssertEqual(result.stats?.truncated, true)
        XCTAssertEqual(result.asText, "old\nnew")
    }

    func testSavedQueriesKeepTheNewestOnce() {
        XCTAssertEqual(logSearchSaved(["a", "b"], adding: " b "), ["b", "a"])
        XCTAssertEqual(logSearchSaved(["a"], adding: "  "), ["a"])
        let full = (1...logSearchMaxSaved).map { "q\($0)" }
        let saved = logSearchSaved(full, adding: "new")
        XCTAssertEqual(saved.count, logSearchMaxSaved)
        XCTAssertEqual(saved.first, "new")
    }

    func testAddsAMatcherToALokiSelector() {
        XCTAssertEqual(
            logSearchQuery(kind: logsKindLoki, "", label: "namespace", value: "demo"), #"{namespace="demo"}"#)
        XCTAssertEqual(
            logSearchQuery(kind: logsKindLoki, #"{pod="a"} |= "x""#, label: "namespace", value: "demo"),
            #"{pod="a",namespace="demo"} |= "x""#)
        XCTAssertEqual(
            logSearchQuery(kind: logsKindLoki, "{}", label: "namespace", value: "demo"), #"{namespace="demo"}"#)
        XCTAssertEqual(logSearchQuery(kind: logsKindLoki, "x", label: "a", value: "b\"c"), #"{a="b\"c"} x"#)
    }

    func testAddsAMatcherToAVictoriaLogsQuery() {
        XCTAssertEqual(
            logSearchQuery(kind: logsKindVictoriaLogs, "", label: "namespace", value: "demo"), #"namespace:"demo""#)
        XCTAssertEqual(
            logSearchQuery(kind: logsKindVictoriaLogs, "timeout", label: "namespace", value: "demo"),
            #"timeout AND namespace:"demo""#)
    }
}

import XCTest

@testable import IchorCore

final class AIToolCallTests: XCTestCase {
    func testArgumentsDecodeFromWhatGoSends() {
        let args = AIToolArgs(json: #"{"namespace":"ns-1","pod":"pod-3","container":"ctr-1","tail":100}"#)
        XCTAssertEqual(args, AIToolArgs(namespace: "ns-1", pod: "pod-3"))
        XCTAssertEqual(AIToolArgs(json: "not json"), AIToolArgs())
    }

    func testAResultClosesTheOldestRequestOfThatTool() {
        let calls = [AIToolCall]()
            .started(tool: "pod_logs", argsJSON: #"{"pod":"pod-3"}"#)
            .started(tool: "pod_logs", argsJSON: #"{"pod":"pod-4"}"#)
        let once = calls.finished(tool: "pod_logs", bytes: 4096, result: "a log", error: nil)
        XCTAssertTrue(once[0].done)
        XCTAssertEqual(once[0].result, "a log")
        XCTAssertFalse(once[1].done)
        let twice = once.finished(tool: "pod_logs", bytes: 0, result: "", error: "not in the report")
        XCTAssertEqual(twice[1].error, "not in the report")
        XCTAssertEqual(twice.finished(tool: "object_yaml", bytes: 1, result: "x", error: nil), twice)
    }

    func testTheSwitchIsOffByDefault() {
        XCTAssertFalse(aiAllowTools(stored: nil))
        XCTAssertFalse(aiAllowTools(stored: false))
        XCTAssertTrue(aiAllowTools(stored: true))
    }
}

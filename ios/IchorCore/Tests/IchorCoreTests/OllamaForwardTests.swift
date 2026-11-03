import XCTest

@testable import IchorCore

final class OllamaForwardTests: XCTestCase {
    func testAServerRoundTripsThroughTheSettings() {
        let ref = OllamaServerRef(namespace: "ai", server: "ollama")
        XCTAssertEqual(ref.text, "ai/ollama")
        XCTAssertEqual(OllamaServerRef(stored: " ai/ollama "), ref)
    }

    func testAnythingElseIsNoServer() {
        for stored in ["", "ollama", "a/b/c", "/ollama", "ai/", "http://127.0.0.1:11434/v1"] {
            XCTAssertNil(OllamaServerRef(stored: stored), stored)
        }
    }

    func testTheCoreAnswerDecodes() throws {
        let json = #"{"baseUrl":"http://127.0.0.1:41234/v1","namespace":"ai","server":"ollama","#
            + #""pod":"ollama-7d9f-x2k4q","port":11434}"#
        let forward = try JSONDecoder().decode(OllamaForward.self, from: Data(json.utf8))
        XCTAssertEqual(forward.baseUrl, "http://127.0.0.1:41234/v1")
        XCTAssertEqual(forward.target, "ai/ollama-7d9f-x2k4q")
        XCTAssertTrue(forward.isFor(OllamaServerRef(namespace: "ai", server: "ollama")))
        XCTAssertFalse(forward.isFor(OllamaServerRef(namespace: "ai", server: "ollama-cpu")))
    }
}

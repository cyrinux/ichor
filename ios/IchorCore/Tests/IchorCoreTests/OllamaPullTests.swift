import XCTest

@testable import IchorCore

final class OllamaPullTests: XCTestCase {
    func testFractionBeforeTheFirstLayer() {
        XCTAssertNil(OllamaPullProgress(status: "pulling manifest").fraction)
    }

    func testFractionIsTheShareDoneClamped() {
        let quarter = OllamaPullProgress(status: "pulling aa", completed: 250, total: 1000)
        XCTAssertEqual(quarter.fraction ?? -1, 0.25, accuracy: 0.0001)
        let over = OllamaPullProgress(status: "success", completed: 1200, total: 1000)
        XCTAssertEqual(over.fraction ?? -1, 1, accuracy: 0.0001)
    }

    func testBytesTexts() {
        let progress = OllamaPullProgress(status: "pulling aa", completed: 1536, total: 397_807_936)
        XCTAssertEqual(progress.completedText, "1.5 KiB")
        XCTAssertEqual(progress.totalText, "379.4 MiB")
    }

    func testModelNamesAsTheCoreAcceptsThem() {
        for name in ["qwen2.5:0.5b", "llama3.2", "hf.co/org/model:q4_k_m"] {
            XCTAssertTrue(isOllamaModelName(name), name)
        }
        for name in ["", "a b", "m:t:x", "../etc", "m;rm", ":tag", "m:", String(repeating: "a", count: 129)] {
            XCTAssertFalse(isOllamaModelName(name), name)
        }
    }

    func testCodable() throws {
        let progress = OllamaPullProgress(status: "success", completed: 68, total: 68)
        let decoded = try JSONDecoder().decode(OllamaPullProgress.self, from: JSONEncoder().encode(progress))
        XCTAssertEqual(decoded, progress)
    }
}

import XCTest
@testable import IchorCore

final class AIDiagnosisTests: XCTestCase {
    func testDecodesProvidersAndModels() throws {
        let providers = try TalosJSON.decode([AIProvider].self, from: """
        [{"id":"anthropic","name":"Anthropic (Claude)","defaultModel":"claude-opus-5-5","keyUrl":"https://console.anthropic.com/settings/keys"},
         {"id":"openai","name":"OpenAI","defaultModel":"gpt-6-astra","keyUrl":"https://platform.openai.com/api-keys"}]
        """)
        XCTAssertEqual(providers.map(\.id), ["anthropic", "openai"])
        XCTAssertEqual(providers[0].defaultModel, "claude-opus-5-5")
        XCTAssertEqual(providers[1].keyUrl, "https://platform.openai.com/api-keys")

        let models = try TalosJSON.decode([AIModel].self, from: """
        [{"id":"claude-opus-5-5","name":"Claude Opus 5.5"},{"id":"llama3","name":"llama3"},{"id":"x","name":""}]
        """)
        XCTAssertEqual(models.map(\.label), ["Claude Opus 5.5 (claude-opus-5-5)", "llama3", "x"])
    }

    func testSelectedProvider() {
        let providers = [
            AIProvider(id: "anthropic", name: "Anthropic (Claude)", defaultModel: "a", keyUrl: ""),
            AIProvider(id: "openai", name: "OpenAI", defaultModel: "b", keyUrl: ""),
        ]
        XCTAssertEqual(selectedAIProvider(providers, saved: "openai")?.id, "openai")
        XCTAssertEqual(selectedAIProvider(providers, saved: nil)?.id, "anthropic")
        XCTAssertEqual(selectedAIProvider(providers, saved: "gone")?.id, "anthropic")
        XCTAssertNil(selectedAIProvider([], saved: "openai"))
    }

    func testCanAskNeedsAKeyOrAServer() {
        XCTAssertFalse(AIProviderSettings().canAsk)
        XCTAssertFalse(AIProviderSettings(apiKey: "  \n", model: "m", baseURL: " ").canAsk)
        XCTAssertTrue(AIProviderSettings(apiKey: "sk-test").canAsk)
        XCTAssertTrue(AIProviderSettings(baseURL: "http://192.0.2.9:11434/v1").canAsk)
    }

    func testKeysArePerProvider() {
        XCTAssertEqual(AIKeys.model("openai"), "aiModel.openai")
        XCTAssertEqual(AIKeys.baseURL("anthropic"), "aiBaseURL.anthropic")
        XCTAssertNotEqual(AIKeys.keychainAccount("anthropic"), AIKeys.keychainAccount("openai"))
    }

    func testAnonymizeDefaultsToOn() {
        XCTAssertTrue(aiAnonymize(stored: nil))
        XCTAssertTrue(aiAnonymize(stored: true))
        XCTAssertFalse(aiAnonymize(stored: false))
    }

    func testHealthFailureNote() {
        XCTAssertEqual(healthFailureNote(" not healthy after 1m0s: waiting for kubelet \n"),
                       "The cluster health check failed: not healthy after 1m0s: waiting for kubelet")
    }

    func testReportSize() {
        XCTAssertEqual(reportSize("abc"), "3 B")
        XCTAssertEqual(reportSize(String(repeating: "é", count: 768)), "1.5 KiB")
    }
}

import XCTest
@testable import IchorCore

final class UpdateBotConfigTests: XCTestCase {
    func testDecodesADraftAndItsNotes() throws {
        let json = #"""
        {"sha":"c1u5t3r","present":[],"draft":{"path":"renovate.json","content":"{\n  \"extends\": [\"config:recommended\"]\n}\n"},
         "notes":["Kubernetes: container images in 1 file under kubernetes/ (for example kubernetes/apps/web/deployment.yaml).",
                  "After merging, install Renovate for this repository on its forge."]}
        """#
        let cfg = try JSONDecoder().decode(UpdateBotConfig.self, from: Data(json.utf8))
        XCTAssertFalse(cfg.configured)
        XCTAssertTrue(cfg.proposable)
        XCTAssertEqual(cfg.draft.path, "renovate.json")
        XCTAssertEqual(cfg.proposalBody.components(separatedBy: "\n").count, 2)
        XCTAssertTrue(cfg.proposalBody.hasPrefix("- Kubernetes: "))
    }

    func testABotConfiguredLeavesNothingToPropose() throws {
        let json = #"""
        {"sha":"abc","present":[{"path":"renovate.json","kind":"renovate"},{"path":".github/dependabot.yml","kind":"dependabot"}],
         "draft":{"path":"","content":""},"notes":["A dependency-update bot is already configured: renovate.json, .github/dependabot.yml."]}
        """#
        let cfg = try JSONDecoder().decode(UpdateBotConfig.self, from: Data(json.utf8))
        XCTAssertTrue(cfg.configured)
        XCTAssertFalse(cfg.proposable)
        XCTAssertEqual(cfg.present.map(\.kind), ["renovate", "dependabot"])
    }

    func testMissingFieldsDecodeEmpty() throws {
        let cfg = try JSONDecoder().decode(UpdateBotConfig.self, from: Data("{}".utf8))
        XCTAssertEqual(cfg, UpdateBotConfig())
        XCTAssertFalse(cfg.proposable)
    }
}

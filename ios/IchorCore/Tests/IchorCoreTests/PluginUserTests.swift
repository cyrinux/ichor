import XCTest
@testable import IchorCore

/// The imported plugins as the Go core lists them (go/plugin/testdata/user, the golden of
/// TestPluginUserListMatchesGolden, read by the Android tests too) and the consent sheet.
final class PluginUserTests: XCTestCase {
    /// The golden, read in place: by this file's path (macOS `swift test`), else from the package
    /// directory test-linux.sh runs in.
    private func golden() throws -> PluginUserList {
        let relative = "go/plugin/testdata/user/plugin-user-list.json"
        let byFile = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
            .deletingLastPathComponent().deletingLastPathComponent()
            .appendingPathComponent(relative)
        let byDirectory = URL(fileURLWithPath: FileManager.default.currentDirectoryPath)
            .appendingPathComponent("../../\(relative)").standardizedFileURL
        let url = try XCTUnwrap(
            [byFile, byDirectory].first { FileManager.default.fileExists(atPath: $0.path) },
            "\(relative) not found")
        return try TalosJSON.decode(
            PluginUserList.self, from: String(decoding: try Data(contentsOf: url), as: UTF8.self))
    }

    func testGoldenDecodes() throws {
        let list = try golden()
        XCTAssertEqual(list.items.map(\.id), ["old-format", "sleep-copy"])
        let broken = list.items[0]
        XCTAssertFalse(broken.usable)
        XCTAssertFalse(broken.enabled)
        XCTAssertTrue(broken.error.contains("not supported"), broken.error)
        XCTAssertEqual(broken.title, "old-format")
        let copy = list.items[1]
        XCTAssertTrue(copy.usable)
        XCTAssertTrue(copy.enabled)
        XCTAssertEqual(copy.title, "kube-green")
        XCTAssertEqual(copy.readsLine, "sleepinfos, secrets")
    }

    func testConsentListsReadsAndActions() throws {
        let consent = try golden().items[1].consent
        XCTAssertEqual(consent.resources.map(\.name), ["sleepinfos.kube-green.com", "secrets"])
        XCTAssertEqual(consent.resources.map(\.secret), [false, true])
        XCTAssertTrue(consent.revealsSecrets)
        XCTAssertFalse(consent.readOnly)
        XCTAssertEqual(
            consent.actions, [PluginConsentAction(label: "Wake up now", verb: "mergePatch", needs: "patch sleepinfos")])
    }

    func testMaskedSecretIsNotFlagged() throws {
        let item = try TalosJSON.decode(
            PluginUserItem.self,
            from: #"""
                {"id":"kube-green-copy","replaces":true,"resources":[
                  {"group":"kube-green.com","resource":"sleepinfos","kind":"SleepInfo"},
                  {"group":"","resource":"secrets","kind":"Secret","reveal":false},
                  {"group":"example.com","resource":"secrets","reveal":true}]}
                """#)
        let consent = item.consent
        XCTAssertEqual(consent.name, "kube-green-copy")
        XCTAssertTrue(consent.replaces)
        XCTAssertFalse(consent.revealsSecrets)
        XCTAssertTrue(consent.readOnly)
        XCTAssertEqual(consent.resources.map(\.secret), [false, true, false])
    }

    func testCatalogSplitsBuiltins() throws {
        let catalog = try TalosJSON.decode(
            PluginCatalog.self, from: #"{"items":[{"id":"kube-green"},{"id":"mine","user":true}]}"#)
        XCTAssertEqual(catalog.builtins.map(\.id), ["kube-green"])
    }
}

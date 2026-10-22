import XCTest
@testable import IchorCore

/// The imported plugins in a backup (Go PluginUserExport / PluginUserRestore, format 5) and a
/// scanned plugin code (Go PluginQRPayload), as the Go core and Android write them.
final class PluginBackupTests: XCTestCase {
    private let export = """
    {"plugins":[{"id":"kube-green-copy","enabled":true,"yaml":"schemaVersion: ichor.dev/plugin/v1alpha1\\nid: kube-green-copy\\n"},
    {"id":"widgets","enabled":false,"yaml":"schemaVersion: ichor.dev/plugin/v1alpha1\\nid: widgets\\n"}]}
    """

    func testExportDecodesAndEncodesBack() throws {
        let backup = try TalosJSON.decode(PluginBackup.self, from: export)
        XCTAssertEqual(backup.plugins.map(\.id), ["kube-green-copy", "widgets"])
        XCTAssertEqual(backup.plugins.map(\.enabled), [true, false])
        XCTAssertTrue(backup.plugins[1].yaml.hasPrefix("schemaVersion:"))
        XCTAssertEqual(try TalosJSON.decode(PluginBackup.self, from: try TalosJSON.encode(backup)), backup)
        XCTAssertEqual(try TalosJSON.decode(PluginBackup.self, from: "{}").plugins, [])
    }

    func testPayloadCarriesPluginsInFormat5Only() throws {
        let plugins = try TalosJSON.decode(PluginBackup.self, from: export)
        XCTAssertEqual(backupPayloadFormat(), backupFormatSettings)
        XCTAssertEqual(backupPayloadFormat(hasProposals: true), backupFormatProposals)
        XCTAssertEqual(backupPayloadFormat(hasPlugins: true), backupFormatPlugins)
        XCTAssertEqual(backupPayloadFormat(hasProposals: true, hasPlugins: true), backupFormatPlugins)
        XCTAssertNil(backupPlugins(PluginBackup()))

        let payload = BackupPayload(format: backupPayloadFormat(hasPlugins: true), talosconfig: "context: a\n",
                                    plugins: backupPlugins(plugins))
        let decoded = try TalosJSON.decode(BackupPayload.self, from: try TalosJSON.encode(payload))
        XCTAssertEqual(restoredPlugins(decoded), plugins)

        // A format 4 payload's plugins are not read; a payload without them leaves the field out.
        var older = decoded
        older.format = backupFormatProposals
        XCTAssertNil(restoredPlugins(older))
        XCTAssertFalse(try TalosJSON.encode(BackupPayload(format: 3, talosconfig: "")).contains("plugins"))
    }

    func testRestoreResultDecodes() throws {
        let result = try TalosJSON.decode(PluginRestoreResult.self, from: """
        {"restored":["kube-green-copy"],"errors":[{"id":"broken","error":"plugin.yaml:2: bad"},{"id":"kube-green","error":"built-in"}]}
        """)
        XCTAssertEqual(result.restored, ["kube-green-copy"])
        XCTAssertEqual(result.skippedLine, "broken: plugin.yaml:2: bad; kube-green: built-in")
        XCTAssertEqual(try TalosJSON.decode(PluginRestoreResult.self, from: "{}").errors, [])
    }

    func testQRPayloadDecodes() throws {
        XCTAssertEqual(try TalosJSON.decode(PluginQRPayload.self, from: #"{"kind":"url","url":"https://example.org/p.yaml"}"#),
                       .url("https://example.org/p.yaml"))
        XCTAssertEqual(try TalosJSON.decode(PluginQRPayload.self, from: #"{"kind":"yaml","yaml":"schemaVersion: x\n"}"#),
                       .yaml("schemaVersion: x\n"))
        XCTAssertThrowsError(try TalosJSON.decode(PluginQRPayload.self, from: #"{"kind":"other"}"#))
    }
}

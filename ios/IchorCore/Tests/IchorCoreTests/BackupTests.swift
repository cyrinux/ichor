import XCTest
@testable import IchorCore

final class BackupTests: XCTestCase {
    func testDecodesAnAndroidPayload() throws {
        let json = """
        {"format":1,"platform":"android","createdAt":1790000000,"talosconfig":"context: lab\\n",
         "activeContextIndex":1,
         "settings":{"themeMode":"black","language":"fr","liveClusterStats":true,"privacyMask":false,
                     "privacyMaskWords":"","monitorAlerts":true,"monitorIntervalMinutes":30,
                     "remoteAppIcons":true},
         "clusters":{"aaaa":{"name":"Home","color":11141120,"vpnOnly":true,"kubeServer":"https://k8s.lan:6443",
                             "wakeOnLan":{"10.0.0.2":{"mac":"aa:bb:cc:dd:ee:ff","broadcast":"","port":9}}}}}
        """
        let payload = try JSONDecoder().decode(BackupPayload.self, from: Data(json.utf8))

        XCTAssertEqual(payload.talosconfig, "context: lab\n")
        XCTAssertEqual(payload.activeContextIndex, 1)
        XCTAssertEqual(payload.settings?.themeMode, "black")
        XCTAssertEqual(payload.settings?.monitorAlerts, true)
        XCTAssertEqual(payload.settings?.remoteAppIcons, true)
        XCTAssertEqual(payload.clusters?["aaaa"], BackupCluster(name: "Home", color: 0xAA0000, vpnOnly: true, kubeServer: "https://k8s.lan:6443"))
    }

    func testMinimalPayloadDecodes() throws {
        let payload = try JSONDecoder().decode(BackupPayload.self, from: Data(#"{"format":1,"talosconfig":"x"}"#.utf8))
        XCTAssertNil(payload.settings)
        XCTAssertNil(payload.clusters)
    }

    func testRoundTripLeavesUnsetFieldsOut() throws {
        let payload = BackupPayload(
            platform: "ios", createdAt: 1_790_000_000, talosconfig: "context: lab\n", activeContextIndex: 0,
            settings: BackupSettings(themeMode: "dark", privacyMask: true, privacyMaskWords: "acme"),
            clusters: ["aaaa": BackupCluster(name: "Home", color: 0x00FF00)]
        )
        let data = try JSONEncoder().encode(payload)
        let json = String(decoding: data, as: UTF8.self)

        XCTAssertEqual(try JSONDecoder().decode(BackupPayload.self, from: data), payload)
        XCTAssertFalse(json.contains("language"))
        XCTAssertFalse(json.contains("vpnOnly"))
        XCTAssertFalse(json.contains("remoteAppIcons"))
        XCTAssertFalse(json.contains("kubeServer"))
    }

    func testBackupClustersKeepsStoredClustersOnly() {
        let clusters = backupClusters(
            fingerprints: ["aaaa", "bbbb", ""],
            names: ["aaaa": "Home", "gone": "Old"],
            colors: ["aaaa": 0xFF00FF, "bbbb": 0x123456],
            kubeServers: ["bbbb": "https://k8s.lan:6443", "gone": "https://old.lan"]
        )
        XCTAssertEqual(clusters, [
            "aaaa": BackupCluster(name: "Home", color: 0xFF00FF),
            "bbbb": BackupCluster(color: 0x123456, kubeServer: "https://k8s.lan:6443"),
        ])
    }

    func testRestoredClustersValidates() {
        let restored = restoredClusters(
            [
                "aaaa": BackupCluster(name: "  Home ", color: -0x1000000 | 0xAA0000),
                "bbbb": BackupCluster(name: "   ", kubeServer: " https://k8s.lan "),
                "gone": BackupCluster(name: "Elsewhere", color: 1, kubeServer: "https://old.lan"),
            ],
            fingerprints: ["aaaa", "bbbb"]
        )
        XCTAssertEqual(restored, RestoredClusters(names: ["aaaa": "Home"], colors: ["aaaa": 0xAA0000],
                                                   kubeServers: ["bbbb": "https://k8s.lan"]))
    }

    func testPassphraseRules() {
        XCTAssertEqual(backupPassphraseProblem("short", again: "short"), .tooShort)
        XCTAssertEqual(backupPassphraseProblem("correct horse", again: "correct horsE"), .mismatch)
        XCTAssertNil(backupPassphraseProblem("correct horse", again: "correct horse"))
        // Characters, not bytes, like the Go core.
        XCTAssertNil(backupPassphraseProblem(String(repeating: "é", count: 12), again: String(repeating: "é", count: 12)))
    }

    func testFileName() {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "UTC")!
        let date = Date(timeIntervalSince1970: 1_790_899_200) // 2026-10-02
        XCTAssertEqual(backupFileName(date: date, calendar: calendar), "ichor-2026-10-02.ichorbackup")
    }

    func testCoreErrorCodes() {
        XCTAssertEqual(BackupError(coreMessage: "backup-wrong-passphrase: wrong passphrase or damaged file"), .wrongPassphrase)
        XCTAssertEqual(BackupError(coreMessage: "backup-unsupported: payload format 2, update the app"), .unsupported)
        XCTAssertEqual(BackupError(coreMessage: "backup-invalid-content: no cluster"), .invalidContent)
        XCTAssertNil(BackupError(coreMessage: "dial tcp: timeout"))
    }
}

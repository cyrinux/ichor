import XCTest
@testable import IchorCore

final class BackupFormat3Tests: XCTestCase {
    private let androidFormat3 = """
        {"format":3,"platform":"android","talosconfig":"context: lab\\n","kubeconfig":"kind: Config\\n",
         "settings":{"themeMode":"dark","allowScreenshots":true,"appLockMode":"securityKey",
                     "storageWarnPercent":80,"storageCriticalPercent":90},
         "pins":{"aaaa":[{"target":{"target":"node","host":"worker-1","addr":"10.0.0.11"},"label":"worker-1"},
                         {"target":{"target":"someday"},"label":"x"},
                         {"target":{"target":"etcd"},"label":"etcd"}],
                 "zzzz":[{"target":{"target":"pod","ns":"a","name":"b"},"label":"b"}]},
         "gitHosts":[{"host":"Git.Example.org","kind":"gitea","aliases":["ssh.git.example.org"]},
                     {"host":"","kind":"github"}],
         "ai":{"enabled":true,"provider":"anthropic","models":{"anthropic":"model-id"},
               "baseUrls":{"openai-compatible":"https://llm.example.org/v1"},"anonymize":false},
         "secrets":{"aiKeys":{"anthropic":"sk-test-key"},"gitHostTokens":{"git.example.org":"tok-test"}},
         "somethingNewer":{"x":1}}
        """

    func testDecodesAFormatTwoPayload() throws {
        let json =
            #"{"format":2,"platform":"ios","talosconfig":"context: lab\n","kubeconfig":"kind: Config\n","settings":{"themeMode":"auto"}}"#
        let payload = try JSONDecoder().decode(BackupPayload.self, from: Data(json.utf8))
        XCTAssertEqual(restoredKubeconfig(payload), "kind: Config\n")
        XCTAssertNil(payload.pins)
        XCTAssertNil(payload.gitHosts)
        XCTAssertNil(payload.ai)
        XCTAssertNil(payload.secrets)
        XCTAssertNil(payload.settings?.appLockMode)
        XCTAssertNil(payload.settings?.storageWarnPercent)
        XCTAssertTrue(restoredPins(payload, fingerprints: ["aaaa"]).isEmpty)
        XCTAssertTrue(restoredGitHosts(payload).isEmpty)
    }

    func testDecodesAFormatThreePayload() throws {
        let payload = try JSONDecoder().decode(BackupPayload.self, from: Data(androidFormat3.utf8))
        XCTAssertEqual(payload.format, 3)
        XCTAssertEqual(restoredKubeconfig(payload), "kind: Config\n")
        XCTAssertEqual(payload.settings?.allowScreenshots, true)
        XCTAssertEqual(payload.settings?.appLockMode, "securityKey")
        XCTAssertEqual(payload.settings?.storageWarnPercent, 80)
        XCTAssertEqual(payload.settings?.storageCriticalPercent, 90)
        XCTAssertEqual(
            payload.ai,
            BackupAI(
                enabled: true, provider: "anthropic", models: ["anthropic": "model-id"],
                baseUrls: ["openai-compatible": "https://llm.example.org/v1"], anonymize: false))
        XCTAssertEqual(payload.secrets?.aiKeys, ["anthropic": "sk-test-key"])

        // The unreadable pin is skipped, the unpinnable one and the unknown cluster dropped on restore.
        XCTAssertEqual(payload.pins?["aaaa"]?.count, 2)
        let pins = restoredPins(payload, fingerprints: ["aaaa"])
        XCTAssertEqual(Array(pins.keys), ["aaaa"])
        XCTAssertEqual(pins["aaaa"]?.pins.map(\.label), ["worker-1"])

        // The empty host is dropped; the host is normalised and gets the backup's token.
        let hosts = restoredGitHosts(payload)
        XCTAssertEqual(hosts.count, 1)
        XCTAssertEqual(
            hosts.first?.host, GitHost(host: "git.example.org", kind: .gitea, aliases: ["ssh.git.example.org"]))
        XCTAssertEqual(hosts.first?.token, "tok-test")
    }

    func testWithoutSecretsTheKeyIsLeftOut() throws {
        XCTAssertNil(
            backupSecrets(
                include: false, aiKeys: ["anthropic": "sk-test-key"], gitHostTokens: ["git.example.org": "tok-secret"]))
        XCTAssertNil(backupSecrets(include: true, aiKeys: ["anthropic": ""], gitHostTokens: [:]))
        let payload = BackupPayload(
            format: backupPayloadFormat(), platform: "ios", talosconfig: "context: lab\n",
            gitHosts: [GitHost(host: "git.example.org", kind: .gitea)],
            ai: BackupAI(enabled: true, provider: "anthropic"),
            secrets: backupSecrets(
                include: false, aiKeys: ["anthropic": "sk-test-key"], gitHostTokens: ["git.example.org": "tok-secret"])
        )
        let json = String(decoding: try JSONEncoder().encode(payload), as: UTF8.self)
        XCTAssertFalse(json.contains("secrets"))
        XCTAssertFalse(json.contains("sk-test-key"))
        XCTAssertFalse(json.contains("tok-secret"))
        XCTAssertTrue(json.contains("git.example.org"))
        // Without a token in the backup the device keeps its own.
        let decoded = try JSONDecoder().decode(BackupPayload.self, from: Data(json.utf8))
        XCTAssertNil(restoredGitHosts(decoded).first?.token)
    }

    func testWithSecretsRoundTrips() throws {
        let secrets = backupSecrets(
            include: true, aiKeys: ["anthropic": "sk-test-key", "openai": ""],
            gitHostTokens: ["git.example.org": "tok-test"])
        XCTAssertEqual(
            secrets, BackupSecrets(aiKeys: ["anthropic": "sk-test-key"], gitHostTokens: ["git.example.org": "tok-test"])
        )
        var settings = BackupSettings(themeMode: "dark")
        settings.appLockMode = BackupLockMode(enabled: true, securityKey: false).rawValue
        settings.storageWarnPercent = 80
        settings.storageCriticalPercent = 92
        let pin = Pin(target: ShareTarget(target: .pod, namespace: "web", name: "site-1"), label: "site-1")
        let payload = BackupPayload(
            format: backupPayloadFormat(), platform: "ios", talosconfig: "context: lab\n", settings: settings,
            pins: backupPins(fingerprints: ["aaaa", "bbbb"]) { $0 == "aaaa" ? PinList([pin]) : PinList() },
            gitHosts: [GitHost(host: "git.example.org", kind: .gitea, aliases: ["ssh.git.example.org"])],
            ai: BackupAI(enabled: true, provider: "anthropic", models: ["anthropic": "model-id"], anonymize: true),
            secrets: secrets
        )
        let data = try JSONEncoder().encode(payload)
        let decoded = try JSONDecoder().decode(BackupPayload.self, from: data)
        XCTAssertEqual(decoded, payload)
        XCTAssertEqual(decoded.settings?.appLockMode, "biometric")
        XCTAssertEqual(decoded.pins.map { Array($0.keys) }, ["aaaa"])
        XCTAssertEqual(restoredGitHosts(decoded).first?.token, "tok-test")
    }

    func testLockMode() {
        XCTAssertEqual(BackupLockMode(enabled: false, securityKey: true), .off)
        XCTAssertEqual(BackupLockMode(enabled: true, securityKey: false), .biometric)
        XCTAssertEqual(BackupLockMode(enabled: true, securityKey: true).rawValue, "securityKey")
    }

    private let known: Set<String> = ["anthropic", "openai-compatible"]
    private let crafted = BackupAI(
        enabled: true, provider: "openai-compatible", models: ["openai-compatible": "m", "unknown": "x"],
        baseUrls: ["openai-compatible": "https://llm.example.org/v1"], anonymize: false)

    func testAIBaseURLWithoutItsKeyIsIgnored() {
        let restored = restoredAI(crafted, keys: nil, known: known)
        XCTAssertTrue(restored.baseURLs.isEmpty)
        XCTAssertNil(restored.anonymize)  // anonymize off needs keys
        XCTAssertEqual(restored.models, ["openai-compatible": "m"])
        XCTAssertEqual(restored.provider, "openai-compatible")

        // A key for another provider, or a blank one, does not unlock this provider's URL.
        let other = restoredAI(crafted, keys: ["anthropic": "sk-test-key", "openai-compatible": "  "], known: known)
        XCTAssertTrue(other.baseURLs.isEmpty)
        XCTAssertEqual(other.keys, ["anthropic": "sk-test-key"])
        XCTAssertEqual(other.anonymize, false)
    }

    func testAIBaseURLWithItsKeyApplies() {
        let restored = restoredAI(crafted, keys: ["openai-compatible": "sk-test-key", "unknown": "k"], known: known)
        XCTAssertEqual(restored.baseURLs, ["openai-compatible": "https://llm.example.org/v1"])
        XCTAssertEqual(restored.keys, ["openai-compatible": "sk-test-key"])
        XCTAssertEqual(restored.anonymize, false)
    }

    func testAIAnonymizeOnAlwaysApplies() {
        let restored = restoredAI(BackupAI(anonymize: true), keys: nil, known: known)
        XCTAssertEqual(restored.anonymize, true)
        XCTAssertNil(restoredAI(nil, keys: nil, known: known).anonymize)
    }
}

import XCTest
@testable import IchorCore

final class KubeConfigDataTests: XCTestCase {
    // A KubeConfigData answer (go/ichorgo/kube_configdata.go).
    private let json = #"""
    {"kind":"Secret","type":"kubernetes.io/tls",
     "keys":[{"key":"tls.crt","size":1200,"hint":"pem","cert":{"subject":"CN=shop.example.com","issuer":"CN=ca.example.com",
              "notBefore":1000,"notAfter":2000000,"dnsNames":["shop.example.com"],"count":2}},
             {"key":"token","size":16,"hint":"text"}],
     "registries":[{"registry":"registry.example.com","username":"shopbot"}],
     "usedBy":[{"pod":"web-1","via":["env","volume"]}],"future":"ignored"}
    """#

    private func data() throws -> KubeConfigData { try TalosJSON.decode(KubeConfigData.self, from: json) }

    func testDecodesTheGoData() throws {
        let d = try data()
        XCTAssertEqual(d.type, "kubernetes.io/tls")
        XCTAssertEqual(d.keys.count, 2)
        XCTAssertEqual(d.keys[0].cert?.count, 2)
        XCTAssertEqual(d.keys[0].cert?.names, ["shop.example.com"])
        XCTAssertNil(d.keys[1].cert)
        XCTAssertFalse(d.keys[1].revealed)
        XCTAssertEqual(d.registries.first?.username, "shopbot")
        XCTAssertEqual(d.usedBy.first?.via, ["env", "volume"])
        XCTAssertFalse(d.usedByUnknown)
    }

    func testRevealedKeyReplacesItsRow() throws {
        let d = try data()
        let shown = ConfigDataKey(key: "token", size: 16, revealed: true, value: "not-a-real-token")
        let rows = d.keys(with: shown)
        XCTAssertEqual(rows[1].value, "not-a-real-token")
        XCTAssertTrue(rows[1].revealed)
        XCTAssertFalse(rows[0].revealed)
        XCTAssertEqual(d.keys(with: nil), d.keys)
    }

    func testCertificateToneByExpiry() {
        let day: Int64 = 86_400
        let cert = ConfigDataCert(subject: "CN=shop.example.com", notAfter: 100 * day)
        XCTAssertEqual(cert.tone(now: 10 * day), .good)
        XCTAssertEqual(cert.tone(now: 90 * day), .warn)
        XCTAssertEqual(cert.tone(now: 100 * day), .bad)
        XCTAssertEqual(cert.daysLeft(now: 100 * day + 1), -1)
        XCTAssertEqual(cert.daysLeft(now: 90 * day), 10)
        XCTAssertEqual(cert.names, ["CN=shop.example.com"])
    }

    func testOnlySecretsAndConfigMapsHaveData() {
        let secret = KubeAPIResource(resource: "secrets", kind: "Secret")
        XCTAssertTrue(secret.hasConfigData)
        XCTAssertEqual(secret.configDataKind, "Secret")
        let configMap = KubeAPIResource(resource: "configmaps", kind: "ConfigMap")
        XCTAssertTrue(configMap.hasConfigData)
        XCTAssertEqual(configMap.configDataKind, "ConfigMap")
        XCTAssertFalse(KubeAPIResource(resource: "pods", kind: "Pod").hasConfigData)
        XCTAssertFalse(KubeAPIResource(group: "example.com", resource: "secrets", kind: "Secret").hasConfigData)
    }
}

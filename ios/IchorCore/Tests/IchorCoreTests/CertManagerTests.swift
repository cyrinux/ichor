import XCTest
@testable import IchorCore

final class CertManagerTests: XCTestCase {
    private let json = #"""
    {"certManager":{"version":"v1","error":"","certificates":[
      {"namespace":"app","name":"old","secretName":"old-tls","dnsNames":["old.example.com"],"dnsNameCount":1,"issuer":"ClusterIssuer/letsencrypt",
       "health":"critical","reasons":["expired"],"ready":false,"message":"Certificate expired","notAfter":1759276800000,"renewalTime":1756684800000,"failedAttempts":5},
      {"namespace":"app","name":"stuck","secretName":"stuck-tls","dnsNames":["stuck.example.com"],"dnsNameCount":1,"issuer":"ClusterIssuer/letsencrypt",
       "health":"warning","reasons":["expiring","renewalOverdue"],"ready":true,"notAfter":1760227200000,"renewalTime":1757635200000},
      {"namespace":"app","name":"internal","secretName":"internal-tls","dnsNames":["a.example.com","b.example.com"],"dnsNameCount":6,"issuer":"Issuer/internal-ca",
       "health":"warning","reasons":["issuer"],"ready":true,"notAfter":1798761600000,"renewalTime":1796083200000},
      {"namespace":"app","name":"web","secretName":"web-tls","dnsNames":["web.example.com"],"dnsNameCount":1,"issuer":"ClusterIssuer/letsencrypt",
       "health":"ok","reasons":[],"ready":true,"notAfter":1796083200000,"renewalTime":1793491200000}],
     "issuers":[
      {"kind":"Issuer","namespace":"app","name":"internal-ca","type":"ca","ready":false,"message":"secret not found","health":"warning"},
      {"kind":"ClusterIssuer","name":"letsencrypt","type":"acme","server":"acme-v02.api.letsencrypt.org","ready":true,"health":"ok"}]}}
    """#

    private var services: DataServices { get throws { try TalosJSON.decode(DataServices.self, from: json) } }

    func testDecodesCertificatesAndIssuers() throws {
        let s = try services
        let cm = try XCTUnwrap(s.certManager)
        XCTAssertEqual(cm.certificates.count, 4)
        XCTAssertEqual(cm.certificates[1].reasons, [.expiring, .renewalOverdue])
        XCTAssertEqual(cm.certificates[2].dnsNameCount, 6)
        XCTAssertEqual(cm.certificates[0].notAfter, 1_759_276_800_000)
        XCTAssertEqual(cm.issuers.map(\.label), ["Issuer/app/internal-ca", "ClusterIssuer/letsencrypt"])
        XCTAssertEqual(cm.issuers[1].server, "acme-v02.api.letsencrypt.org")
        XCTAssertEqual(s.detected, [.certManager])
    }

    func testSummaryCountsIssuersToo() throws {
        // Three certificates and the CA issuer need a look.
        XCTAssertEqual(try services.summary(.certManager), ServiceSummary(total: 4, attention: 4, health: .critical))
        XCTAssertEqual(try services.likelyCauses(downNodes: ["node-1"]), [])
    }

    func testAlertsLeaveTheIssuerReasonToTheIssuer() throws {
        XCTAssertEqual(dataIssuesOf(try services), [
            "certmanager|app/old": dataCritical,
            "certmanager|app/stuck": dataWarning,
            "certmanager|Issuer/app/internal-ca": dataWarning,
        ])
    }

    func testHintsIncludeCertManager() throws {
        let inventory = try TalosJSON.decode(ClusterInventory.self, from: #"{"apps":[{"id":"cert-manager","name":"cert-manager"},{"id":"longhorn","name":"Longhorn"}]}"#)
        XCTAssertEqual(dataServiceHints(inventory), "longhorn,cert-manager")
    }
}

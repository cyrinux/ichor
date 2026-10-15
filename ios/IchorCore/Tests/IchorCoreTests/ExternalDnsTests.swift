import XCTest
@testable import IchorCore

final class ExternalDnsTests: XCTestCase {
    private let json = #"""
    {"externalDns":{"version":"v1alpha1","error":"",
     "controllers":[
      {"namespace":"external-dns","name":"external-dns","ready":true,"replicas":1,"readyReplicas":1,
       "provider":"cloudflare","policy":"sync","registry":"txt","ownerId":"homelab",
       "domainFilters":["example.com"],"sources":["ingress","service","crd"],"dryRun":false,
       "intervalSeconds":30,"annotationFilter":"","health":"ok","reasons":[]},
      {"namespace":"dns-2","name":"legacy","ready":false,"replicas":2,"readyReplicas":0,
       "provider":"inmemory","policy":"","registry":"txt","ownerId":"legacy",
       "domainFilters":["example.org"],"sources":["ingress"],"dryRun":true,
       "intervalSeconds":60,"annotationFilter":"","health":"critical","reasons":["notReady","dryRun"]}],
     "records":[
      {"dnsName":"*.example.net","recordType":"A","targets":["203.0.113.50"],"ttl":300,
       "sourceKind":"Ingress","sourceNamespace":"demo","sourceName":"legacy",
       "sourceGroup":"networking.k8s.io","sourceVersion":"v1","sourceResource":"ingresses",
       "controller":"","health":"warning","reasons":["unmanaged"]},
      {"dnsName":"db.example.com","recordType":"A","targets":["203.0.113.40"],"ttl":300,
       "sourceKind":"DNSEndpoint","sourceNamespace":"demo","sourceName":"db",
       "sourceGroup":"externaldns.k8s.io","sourceVersion":"v1alpha1","sourceResource":"dnsendpoints",
       "controller":"external-dns","health":"warning","reasons":["stale"]},
      {"dnsName":"status.example.com","recordType":"A","targets":[],"ttl":0,
       "sourceKind":"Ingress","sourceNamespace":"demo","sourceName":"status",
       "sourceGroup":"networking.k8s.io","sourceVersion":"v1","sourceResource":"ingresses",
       "controller":"external-dns","health":"warning","reasons":["noTarget"]},
      {"dnsName":"app.example.com","recordType":"A","targets":["203.0.113.10"],"ttl":300,
       "sourceKind":"Ingress","sourceNamespace":"demo","sourceName":"app",
       "sourceGroup":"networking.k8s.io","sourceVersion":"v1","sourceResource":"ingresses",
       "controller":"external-dns","health":"ok","reasons":[]},
      {"dnsName":"extra.example.com","recordType":"CNAME","targets":["lb.example.com"],"ttl":300,
       "sourceKind":"Ingress","sourceNamespace":"demo","sourceName":"extra",
       "sourceGroup":"networking.k8s.io","sourceVersion":"v1","sourceResource":"ingresses",
       "controller":"external-dns","health":"ok","reasons":[]}],
     "recordCount":5,"truncated":false}}
    """#

    private var services: DataServices { get throws { try TalosJSON.decode(DataServices.self, from: json) } }

    func testDecodesControllersAndRecords() throws {
        let s = try services
        let e = try XCTUnwrap(s.externalDns)
        XCTAssertEqual(e.version, "v1alpha1")
        XCTAssertEqual(e.controllers.count, 2)
        let main = e.controllers[0]
        XCTAssertEqual(main.provider, "cloudflare")
        XCTAssertEqual(main.policy, "sync")
        XCTAssertEqual(main.domainFilters, ["example.com"])
        XCTAssertEqual(main.intervalSeconds, 30)
        XCTAssertEqual(main.health, .ok)
        let legacy = e.controllers[1]
        XCTAssertEqual(legacy.health, .critical)
        XCTAssertEqual(legacy.reasons, [.notReady, .dryRun])
        XCTAssertEqual(e.recordCount, 5)
        // Problems first: the unmanaged name, then the stale DNSEndpoint, then the ok ones.
        XCTAssertEqual(e.records[0].dnsName, "*.example.net")
        XCTAssertEqual(e.records[0].reasons, [.unmanaged])
        XCTAssertEqual(e.records[1].sourceLabel, "DNSEndpoint/demo/db")
        XCTAssertNil(e.records[0].controller.nonEmpty)
        XCTAssertEqual(e.records[4].recordType, "CNAME")
        XCTAssertEqual(s.detected, [.externalDns])
    }

    func testSummaryCountsRecordsAndLooksAtControllers() throws {
        // 5 records, of which 3 need a look (unmanaged, stale, no target) plus the critical controller.
        XCTAssertEqual(try services.summary(.externalDns), ServiceSummary(total: 5, attention: 4, health: .critical))
    }

    func testAlertsCoverCriticalControllersOnly() throws {
        XCTAssertEqual(try dataIssuesOf(services), ["externaldns|dns-2/legacy": dataCritical])
    }

    func testSystemTitle() {
        XCTAssertEqual(dataSystemTitle("externaldns|dns-2/legacy"), "ExternalDNS")
    }
}

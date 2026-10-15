import XCTest
@testable import IchorCore

final class TailscaleTests: XCTestCase {
    // Go KubeTailscale's demo answer.
    private let json = #"""
    {"installed":true,"version":"v1alpha1","truncated":false,
     "operator":{"namespace":"tailscale","name":"operator","ready":1,"desired":1,"image":"tailscale/k8s-operator:v1.90.0","health":"ok"},
     "connectors":[
      {"name":"saas-connector","hostname":"saas-connector","tailnetIPs":[],"subnetRoutes":"","advertisedRoutes":[],"exitNode":false,
       "appConnector":true,"appConnectorRoutes":["203.0.113.0/24"],"tags":["tag:k8s"],"proxyClass":"saas","replicas":1,"devices":[],
       "ready":false,"reason":"ProxyClassMissing","message":"ProxyClass \"saas\" does not exist","health":"critical"},
      {"name":"exit-eu","hostname":"exit-eu","tailnetIPs":["100.64.0.21"],"subnetRoutes":"","advertisedRoutes":[],"exitNode":true,
       "appConnector":false,"appConnectorRoutes":[],"tags":["tag:k8s"],"proxyClass":"","replicas":1,
       "devices":[{"hostname":"exit-eu","tailnetIPs":["100.64.0.21"],"staticEndpoints":[]}],"ready":true,"reason":"","message":"","health":"ok"},
      {"name":"office-router","hostname":"office-router","tailnetIPs":["100.64.0.11","fd7a:115c:a1e0::b"],"subnetRoutes":"10.0.0.0/24",
       "advertisedRoutes":["10.0.0.0/24"],"exitNode":false,"appConnector":false,"appConnectorRoutes":[],"tags":["tag:k8s"],"proxyClass":"prod","replicas":2,
       "devices":[{"hostname":"office-router-0","tailnetIPs":["100.64.0.11"],"staticEndpoints":[]},{"hostname":"office-router-1","tailnetIPs":["100.64.0.12"],"staticEndpoints":[]}],
       "ready":true,"reason":"","message":"","health":"ok"}],
     "proxyGroups":[
      {"name":"api-proxies","type":"kube-apiserver","replicas":2,"proxyClass":"","tags":["tag:k8s"],"url":"https://api-proxies.example.ts.net",
       "devices":[{"hostname":"api-proxies-0","tailnetIPs":["100.64.0.41"],"staticEndpoints":[]},{"hostname":"api-proxies-1","tailnetIPs":["100.64.0.42"],"staticEndpoints":[]}],
       "available":true,"ready":true,"reason":"","message":"","health":"ok"},
      {"name":"ingress-proxies","type":"ingress","replicas":2,"proxyClass":"prod","tags":["tag:k8s"],"url":"",
       "devices":[{"hostname":"ingress-proxies-0","tailnetIPs":["100.64.0.31"],"staticEndpoints":[]},{"hostname":"ingress-proxies-1","tailnetIPs":["100.64.0.32"],"staticEndpoints":[]}],
       "available":true,"ready":true,"reason":"","message":"","health":"ok"}],
     "proxyClasses":[
      {"name":"metrics","usedBy":0,"ready":false,"reason":"ProxyClassInvalid","message":"spec.metrics.serviceMonitor needs the prometheus-operator CRDs","health":"warning"},
      {"name":"prod","usedBy":2,"ready":true,"reason":"","message":"","health":"ok"}],
     "dnsConfig":{"name":"ts-dns","nameserverIP":"100.100.100.100","ready":true,"reason":"","message":"","health":"ok"},
     "counts":{"ok":7,"warning":1,"critical":1}}
    """#

    private func decode(_ text: String) throws -> Tailscale {
        try JSONDecoder().decode(Tailscale.self, from: Data(text.utf8))
    }

    func testDecodesEveryKind() throws {
        let ts = try decode(json)
        XCTAssertTrue(ts.installed)
        XCTAssertEqual(ts.operator?.name, "operator")
        XCTAssertEqual(ts.operator?.health, .ok)
        XCTAssertEqual(ts.connectors.count, 3)
        XCTAssertEqual(ts.proxyGroups.first?.url, "https://api-proxies.example.ts.net")
        XCTAssertEqual(ts.proxyClasses.last?.usedBy, 2)
        XCTAssertEqual(ts.dnsConfig?.nameserverIP, "100.100.100.100")
        XCTAssertEqual(ts.counts, TailscaleCounts(ok: 7, warning: 1, critical: 1))
    }

    func testProblemsComeFirstWithTheirReason() throws {
        let ts = try decode(json)
        let app = try XCTUnwrap(ts.connectors.first)
        XCTAssertEqual(app.state.health, .critical)
        XCTAssertEqual(app.state.reason, "ProxyClassMissing")
        XCTAssertTrue(app.appConnector)
        XCTAssertEqual(ts.proxyClasses.first?.state.health, .warning)
    }

    func testRoutesFallBackToTheStatus() throws {
        let ts = try decode(json)
        XCTAssertEqual(ts.connectors[2].routes, ["10.0.0.0/24"])
        XCTAssertEqual(ts.connectors[1].routes, [])
        let fromStatus = try decode(#"{"installed":true,"connectors":[{"name":"r","subnetRoutes":"10.0.0.0/24, 10.1.0.0/16"}]}"#)
        XCTAssertEqual(fromStatus.connectors.first?.routes, ["10.0.0.0/24", "10.1.0.0/16"])
    }

    func testAnOlderOrEmptyAnswerStillDecodes() throws {
        let absent = try decode(#"{"installed":false}"#)
        XCTAssertFalse(absent.installed)
        XCTAssertNil(absent.operator)
        XCTAssertTrue(absent.isEmpty)
        XCTAssertEqual(absent.apiVersion, "v1alpha1")
        XCTAssertEqual(TailscaleKind.dnsConfig.resource, "dnsconfigs")
    }
}

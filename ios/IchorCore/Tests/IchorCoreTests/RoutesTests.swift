import XCTest
@testable import IchorCore

final class RoutesTests: XCTestCase {
    func testDecodesTheGoJson() throws {
        let json = #"{"routes":[{"kind":"HTTPRoute","namespace":"media","name":"jellyfin","url":"https://jellyfin.home.example/web","service":"jellyfin"},"#
            + #"{"kind":"Ingress","namespace":"demo","name":"hello","url":"http://hello.example"}]}"#
        let routes = try JSONDecoder().decode(KubeRouteList.self, from: Data(json.utf8)).routes
        XCTAssertEqual(routes.map(\.label), ["jellyfin.home.example/web", "hello.example"])
        XCTAssertEqual(routes[1].service, "")
        XCTAssertEqual(try JSONDecoder().decode(KubeRouteList.self, from: Data("{}".utf8)).routes, [])
    }

    func testDecodesATraefikRoute() throws {
        let json = #"{"routes":[{"kind":"IngressRoute","namespace":"monitoring","name":"grafana","url":"https://grafana.home.example","service":"grafana"}]}"#
        let route = try XCTUnwrap(try JSONDecoder().decode(KubeRouteList.self, from: Data(json.utf8)).routes.first)
        XCTAssertEqual(route.kind, "IngressRoute")
        XCTAssertEqual(route.label, "grafana.home.example")
    }

    func testDecodesAGatewayRouteWithoutPath() throws {
        let json = #"{"routes":[{"kind":"GRPCRoute","namespace":"shop","name":"api","url":"https://api.shop.example","service":"web"},"#
            + #"{"kind":"TLSRoute","namespace":"home","name":"mosquitto","url":"https://mqtt.home.example","service":"mosquitto"}]}"#
        let routes = try JSONDecoder().decode(KubeRouteList.self, from: Data(json.utf8)).routes
        XCTAssertEqual(routes.map(\.kind), ["GRPCRoute", "TLSRoute"])
        XCTAssertEqual(routes.map(\.label), ["api.shop.example", "mqtt.home.example"])
    }

    func testCarriesWhereTheRouteIsRead() throws {
        let json = #"{"routes":[{"kind":"HTTPRoute","namespace":"media","name":"jellyfin","url":"https://jellyfin.home.example","#
            + #""service":"jellyfin","group":"gateway.networking.k8s.io","version":"v1","resource":"httproutes"}]}"#
        let route = try XCTUnwrap(try JSONDecoder().decode(KubeRouteList.self, from: Data(json.utf8)).routes.first)
        XCTAssertEqual(route.apiResource,
                       KubeAPIResource(group: "gateway.networking.k8s.io", version: "v1", resource: "httproutes", kind: "HTTPRoute"))
    }

    func testARouteWithoutItsResourceHasNoObject() throws {
        let json = #"{"routes":[{"kind":"Ingress","namespace":"demo","name":"hello","url":"http://hello.example"}]}"#
        let route = try XCTUnwrap(try JSONDecoder().decode(KubeRouteList.self, from: Data(json.utf8)).routes.first)
        XCTAssertNil(route.apiResource)
    }

    func testRoutePodsOncePerPod() throws {
        let app = InventoryApp(id: "web", name: "Web", pods: [
            InventoryPod(namespace: "shop", pod: "web-0", node: "n1"),
            InventoryPod(namespace: "shop", pod: "web-0", node: "n2"),
            InventoryPod(namespace: "shop", pod: "web-1", node: "n1"),
        ])
        XCTAssertEqual(app.routePods, [RoutePod(namespace: "shop", pod: "web-0"), RoutePod(namespace: "shop", pod: "web-1")])
        let encoded = String(decoding: try JSONEncoder().encode([app.routePods[0]]), as: UTF8.self)
        XCTAssertTrue(encoded.contains(#""namespace":"shop""#) && encoded.contains(#""pod":"web-0""#), encoded)
    }

    func testCertificateFieldsDecodeAndDefault() throws {
        let list = try JSONDecoder().decode(KubeRouteList.self, from: Data(#"""
        {"routes":[
          {"kind":"Ingress","namespace":"default","name":"vaultwarden","url":"https://vault.home.example","certNotAfter":1000000,"certIssue":"expiring","certSecret":"default/vaultwarden-tls"},
          {"kind":"Ingress","namespace":"default","name":"paperless","url":"https://paperless.home.example","certIssue":"hostMismatch","certManaged":true},
          {"kind":"Ingress","namespace":"demo","name":"hello","url":"https://hello.home.example"}]}
        """#.utf8))
        let routes = list.routes
        XCTAssertEqual(routes[0].certHealth, .warning)
        XCTAssertEqual(routes[0].certSecret, "default/vaultwarden-tls")
        XCTAssertEqual(routes[0].certDaysLeft(now: 1_000_000 - 9 * 86_400 - 3_600), 9)
        XCTAssertEqual(routes[1].certHealth, .warning)
        XCTAssertTrue(routes[1].certManaged)
        XCTAssertNil(routes[2].certHealth)
        XCTAssertEqual(routes[2].certNotAfter, 0)
    }
}

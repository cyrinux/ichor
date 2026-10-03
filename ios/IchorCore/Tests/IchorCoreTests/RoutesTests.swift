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
}

import XCTest
@testable import TalosdevMobileCore

final class NetworkTests: XCTestCase {
    func testDecodeGoJSON() throws {
        let json = #"""
        {"links":[{"name":"eth0","type":"ether","kind":"","state":"up","hardwareAddr":"aa:bb:cc:dd:ee:ff","mtu":1500,"speedMbit":10000,"virtual":false},
        {"name":"lxc123","type":"ether","kind":"veth","state":"up","hardwareAddr":"","mtu":1450,"speedMbit":0,"virtual":true}],
        "addresses":[{"address":"10.0.0.2/24","link":"eth0","family":"inet4","scope":"global","virtual":false}],
        "routes":[{"destination":"default","gateway":"10.0.0.1","link":"eth0","metric":1024,"table":"main","family":"inet4","virtual":false},
        {"destination":"10.244.1.5/32","gateway":"","link":"lxc123","metric":0,"table":"main","family":"inet4","virtual":true}],
        "resolvers":["1.1.1.1"],"timeServers":["time.cloudflare.com"],"errors":{"routes":"permission denied"}}
        """#
        let net = try TalosJSON.decode(NodeNetwork.self, from: json)
        XCTAssertEqual(net.links.count, 2)
        XCTAssertEqual(net.links[0].speedMbit, 10_000)
        XCTAssertTrue(net.links[1].virtual)
        XCTAssertEqual(net.addresses.first?.address, "10.0.0.2/24")
        XCTAssertTrue(net.routes[0].isDefault)
        XCTAssertEqual(net.errors["routes"], "permission denied")
        XCTAssertEqual(net.timeServers, ["time.cloudflare.com"])
    }

    func testNullSectionsDecodeAsEmpty() throws {
        let net = try TalosJSON.decode(NodeNetwork.self, from: #"{"links":null,"addresses":null,"routes":null,"resolvers":null,"timeServers":null,"errors":null}"#)
        XCTAssertEqual(net, NodeNetwork())
    }

    func testVirtualFiltering() {
        let links = [NetLink(name: "eth0"), NetLink(name: "lxc1", kind: "veth", virtual: true)]
        XCTAssertEqual(visibleLinks(links, showVirtual: false).map(\.name), ["eth0"])
        XCTAssertEqual(visibleLinks(links, showVirtual: true).count, 2)
        let addresses = [NetAddress(address: "10.0.0.2/24", link: "eth0"), NetAddress(address: "10.244.0.1/32", link: "cilium_host", virtual: true)]
        XCTAssertEqual(visibleAddresses(addresses, showVirtual: false).map(\.link), ["eth0"])
        let routes = [
            NetRoute(destination: "default", link: "cilium_host", virtual: true),
            NetRoute(destination: "10.244.1.5/32", link: "lxc1", virtual: true),
            NetRoute(destination: "10.0.0.0/24", link: "eth0"),
        ]
        XCTAssertEqual(visibleRoutes(routes, showVirtual: false).map(\.destination), ["default", "10.0.0.0/24"])
    }

    func testLinkSpeed() {
        XCTAssertNil(formatLinkSpeed(0))
        XCTAssertEqual(formatLinkSpeed(100), "100 Mb/s")
        XCTAssertEqual(formatLinkSpeed(10_000), "10 Gb/s")
        XCTAssertEqual(formatLinkSpeed(2_500), "2.5 Gb/s")
    }

    func testDecodeConnectionsWithoutProcess() throws {
        let json = #"[{"protocol":"tcp","localIp":"0.0.0.0","localPort":50000,"remoteIp":"0.0.0.0","remotePort":0,"state":"LISTEN","listening":true,"pid":1,"processName":"machined"},{"protocol":"udp6","localIp":"::","localPort":123,"remoteIp":"::","remotePort":0,"state":"CLOSE","listening":true}]"#
        let list = try TalosJSON.decode([NodeConnection].self, from: json)
        XCTAssertEqual(list[0].processName, "machined")
        XCTAssertEqual(list[1].pid, 0)
        XCTAssertEqual(list[1].localEndpoint, "[::]:123")
        XCTAssertEqual(list[0].localEndpoint, "0.0.0.0:50000")
    }

    func testFilterConnections() {
        let list = [
            NodeConnection(localPort: 6443, pid: 42, processName: "kube-apiserver"),
            NodeConnection(localIp: "10.0.0.2", localPort: 40000, remoteIp: "10.0.0.3", remotePort: 2379, state: "ESTABLISHED",
                           listening: false, pid: 7, processName: "etcd"),
        ]
        XCTAssertEqual(filterConnections(list, filter: .listening, query: "").map(\.localPort), [6443])
        XCTAssertEqual(filterConnections(list, filter: .all, query: "").count, 2)
        XCTAssertEqual(filterConnections(list, filter: .all, query: "ETCD").map(\.localPort), [40000])
        XCTAssertEqual(filterConnections(list, filter: .all, query: "2379").map(\.localPort), [40000])
        XCTAssertEqual(filterConnections(list, filter: .all, query: "42").map(\.localPort), [6443])
        XCTAssertEqual(filterConnections(list, filter: .listening, query: "etcd"), [])
    }
}

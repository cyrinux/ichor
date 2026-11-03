import XCTest
@testable import IchorCore

final class NetMapTests: XCTestCase {
    // Shaped like KubeArgoNetwork's demo answer for demo-worker (go/ichorgo/kube_argocd_network_demo.go),
    // with a shared Gateway and an HTTPRoute in front: a pod crash-loops on a node that is not ready.
    private let json = #"""
        {"nodes":[
          {"id":"host/https://worker.homelab.lan","layer":0,"kind":"Host","namespace":"","name":"worker.homelab.lan","detail":"https://worker.homelab.lan","health":"ok","url":"https://worker.homelab.lan","managed":false},
          {"id":"gw/traefik/homelab","layer":1,"kind":"Gateway","namespace":"traefik","name":"homelab","detail":"10.0.0.240","health":"ok","url":"","managed":false},
          {"id":"hr/demo/worker","layer":2,"kind":"HTTPRoute","namespace":"demo","name":"worker","detail":"","health":"ok","url":"","managed":true},
          {"id":"svc/demo/worker","layer":3,"kind":"Service","namespace":"demo","name":"worker","detail":"ClusterIP 10.96.31.7 · 9090→metrics","health":"warning","url":"","managed":true},
          {"id":"pod/demo/worker-6f4b8-uvwxy","layer":4,"kind":"Pod","namespace":"demo","name":"worker-6f4b8-uvwxy","detail":"CrashLoopBackOff · 14 restarts","health":"critical","url":"","managed":false},
          {"id":"pod/demo/worker-6f4b8-pqrst","layer":4,"kind":"Pod","namespace":"demo","name":"worker-6f4b8-pqrst","detail":"Running · 1/1 ready","health":"ok","url":"","managed":false},
          {"id":"node/demo-worker-3","layer":5,"kind":"Node","namespace":"","name":"demo-worker-3","detail":"NotReady","health":"critical","url":"","managed":false},
          {"id":"node/demo-worker-2","layer":5,"kind":"Node","namespace":"","name":"demo-worker-2","detail":"","health":"ok","url":"","managed":false}],
         "edges":[
          {"from":"host/https://worker.homelab.lan","to":"gw/traefik/homelab","health":"ok"},
          {"from":"gw/traefik/homelab","to":"hr/demo/worker","health":"ok"},
          {"from":"hr/demo/worker","to":"svc/demo/worker","health":"warning"},
          {"from":"svc/demo/worker","to":"pod/demo/worker-6f4b8-uvwxy","health":"critical"},
          {"from":"svc/demo/worker","to":"pod/demo/worker-6f4b8-pqrst","health":"ok"},
          {"from":"pod/demo/worker-6f4b8-uvwxy","to":"node/demo-worker-3","health":"critical"},
          {"from":"pod/demo/worker-6f4b8-pqrst","to":"node/demo-worker-2","health":"ok"}],
         "problem":{"kind":"Node","namespace":"","name":"demo-worker-3","detail":"NotReady"}}
        """#

    private func decode(_ text: String) throws -> NetMap {
        try JSONDecoder().decode(NetMap.self, from: Data(text.utf8))
    }

    func testDecodesTheDemoShape() throws {
        let net = try decode(json)
        XCTAssertEqual(net.nodes.count, 8)
        XCTAssertEqual(net.edges.count, 7)
        let gateway = try XCTUnwrap(net.node("gw/traefik/homelab"))
        XCTAssertEqual(gateway.layer, 1)
        XCTAssertEqual(gateway.kind, .gateway)
        XCTAssertTrue(gateway.shared)
        XCTAssertFalse(net.node("hr/demo/worker")?.shared ?? true)
        XCTAssertFalse(net.node("pod/demo/worker-6f4b8-pqrst")?.shared ?? true, "a pod is never drawn as shared")
        XCTAssertEqual(net.node("host/https://worker.homelab.lan")?.url, "https://worker.homelab.lan")
        XCTAssertEqual(net.node("svc/demo/worker")?.health, .warning)
        XCTAssertEqual(net.edges[3].health, .critical)
        XCTAssertEqual(net.problem, NetMapProblem(kind: .node, name: "demo-worker-3", detail: "NotReady"))
    }

    func testDecodesTolerantly() throws {
        let net = try decode(#"{"nodes":[{"id":"x","layer":9,"kind":"Mystery","name":"x"}],"problem":null}"#)
        XCTAssertEqual(net.nodes.first?.layer, 9, "a caller's own layer is kept")
        XCTAssertEqual(net.nodes.first?.kind, .unknown)
        XCTAssertEqual(net.nodes.first?.kindName, "Mystery", "its column is titled with it")
        XCTAssertEqual(net.nodes.first?.health, .unknown)
        XCTAssertTrue(net.edges.isEmpty)
        XCTAssertNil(net.problem)
        XCTAssertTrue(try decode("{}").isEmpty)
    }

    func testColumnsSkipEmptyLayers() throws {
        let net = try decode(json)
        XCTAssertEqual(net.columns().map(\.layer), [0, 1, 2, 3, 4, 5])
        let noGateway = NetMap(nodes: net.nodes.filter { $0.kind != .gateway && $0.kind != .httpRoute })
        XCTAssertEqual(noGateway.columns().map(\.layer), [0, 3, 4, 5])
        XCTAssertEqual(noGateway.columns()[2].nodes.first?.name, "worker-6f4b8-uvwxy", "worst first, as sent")
        XCTAssertTrue(NetMap().columns().isEmpty)
    }

    func testAnyLongColumnFoldsBehindItsOwnBox() {
        let services = (0..<10).map {
            NetMapNode(id: "svc/ns/s\($0)", layer: 3, kind: .service, namespace: "ns", name: "s\($0)")
        }
        let net = NetMap(nodes: services)
        let column = net.columns()[0]
        XCTAssertTrue(column.folds)
        XCTAssertEqual(column.nodes.count, 8)
        XCTAssertEqual(column.hidden.map(\.name), ["s8", "s9"])
        XCTAssertEqual(column.kind, .service)
        XCTAssertTrue(net.columns(expanded: true)[0].folds, "still folds back once open")
        XCTAssertEqual(NetMap.moreID(layer: 3), "more/3")
    }

    func testPodsBeyondTheLimitFold() {
        let pods = (0..<11).map {
            NetMapNode(id: "pod/ns/p\($0)", layer: 4, kind: .pod, namespace: "ns", name: "p\($0)")
        }
        let node = NetMapNode(id: "node/n1", layer: 5, kind: .node, name: "n1")
        let svc = NetMapNode(id: "svc/ns/s", layer: 3, kind: .service, namespace: "ns", name: "s")
        let edges = pods.flatMap {
            [
                NetMapEdge(from: svc.id, to: $0.id),
                NetMapEdge(from: $0.id, to: node.id, health: $0.name == "p10" ? .critical : .ok),
            ]
        }
        let net = NetMap(nodes: [svc] + pods + [node], edges: edges)

        let column = net.columns()[1]
        XCTAssertEqual(column.nodes.count, 8)
        XCTAssertEqual(column.hidden.map(\.name), ["p8", "p9", "p10"])
        XCTAssertTrue(net.columns(expanded: true)[1].hidden.isEmpty)

        let hidden = Set(column.hidden.map(\.id))
        let shown = net.edges(hiding: hidden)
        XCTAssertEqual(shown.count, 16 + 2, "the folded pods share one hop in, one hop out")
        let more = NetMap.moreID(layer: 4)
        XCTAssertEqual(shown.first { $0.from == more }?.health, .critical, "the worst they stand for")

        let path = net.path(through: "pod/ns/p9", hiding: hidden)
        XCTAssertTrue(path.contains(node: NetMap.moreID(layer: 4)))
        XCTAssertTrue(path.contains(edge: "svc/ns/s>\(NetMap.moreID(layer: 4))"))
        XCTAssertTrue(path.contains(edge: "\(NetMap.moreID(layer: 4))>node/n1"))
        XCTAssertFalse(path.contains(node: "pod/ns/p0"))
    }

    func testPathThroughAServiceLightsBothWays() throws {
        let path = try decode(json).path(through: "svc/demo/worker")
        XCTAssertEqual(
            path.nodes,
            [
                "host/https://worker.homelab.lan", "gw/traefik/homelab", "hr/demo/worker", "svc/demo/worker",
                "pod/demo/worker-6f4b8-uvwxy", "pod/demo/worker-6f4b8-pqrst", "node/demo-worker-3",
                "node/demo-worker-2",
            ])
        XCTAssertEqual(path.edges.count, 7)
    }

    func testPathThroughAPodSkipsItsSiblings() throws {
        let path = try decode(json).path(through: "pod/demo/worker-6f4b8-uvwxy")
        XCTAssertTrue(path.contains(node: "host/https://worker.homelab.lan"))
        XCTAssertTrue(path.contains(node: "node/demo-worker-3"))
        XCTAssertFalse(path.contains(node: "pod/demo/worker-6f4b8-pqrst"))
        XCTAssertFalse(path.contains(node: "node/demo-worker-2"))
        XCTAssertTrue(path.contains(edge: "svc/demo/worker>pod/demo/worker-6f4b8-uvwxy"))
        XCTAssertFalse(path.contains(edge: "svc/demo/worker>pod/demo/worker-6f4b8-pqrst"))
    }

    func testPathThroughANodeReachesTheHosts() throws {
        let path = try decode(json).path(through: "node/demo-worker-2")
        XCTAssertEqual(
            path.nodes,
            [
                "node/demo-worker-2", "pod/demo/worker-6f4b8-pqrst", "svc/demo/worker", "hr/demo/worker",
                "gw/traefik/homelab", "host/https://worker.homelab.lan",
            ])
    }

    func testTalosDownNodesTurnCritical() throws {
        let healthy = NetMap(
            nodes: try decode(json).nodes.filter { $0.name != "demo-worker-3" },
            edges: [NetMapEdge(from: "pod/demo/worker-6f4b8-pqrst", to: "node/demo-worker-2")],
            problem: NetMapProblem(
                kind: .pod, namespace: "demo", name: "worker-6f4b8-uvwxy", detail: "CrashLoopBackOff"))
        let marked = healthy.marking(down: ["demo-worker-2"])
        XCTAssertEqual(marked.node("node/demo-worker-2")?.health, .critical)
        XCTAssertEqual(marked.node("node/demo-worker-2")?.detail, "NotReady")
        XCTAssertEqual(marked.edges.first?.health, .critical)
        XCTAssertEqual(marked.problem?.reason, .nodeNotReady("demo-worker-2"), "the deepest cause wins")
        XCTAssertEqual(healthy.marking(down: ["elsewhere"]), healthy)
        XCTAssertEqual(try decode(json).marking(down: ["demo-worker-3"]), try decode(json), "already critical")
    }

    func testDecodesTraefikRoutes() throws {
        let net = try decode(
            #"""
            {"nodes":[
              {"id":"ir/monitoring/grafana","layer":2,"kind":"IngressRoute","namespace":"monitoring","name":"grafana","detail":"10.0.0.240","health":"ok","url":"","managed":true},
              {"id":"irtcp/edge/web","layer":2,"kind":"IngressRouteTCP","namespace":"edge","name":"web","detail":"","health":"ok","url":"","managed":false}],
             "edges":[]}
            """#)
        XCTAssertEqual(net.nodes.map(\.kind), [.ingressRoute, .ingressRouteTCP])
        XCTAssertTrue(net.nodes.allSatisfy { $0.kind.routesTraffic })
        XCTAssertFalse(NetMapKind.service.routesTraffic)
    }

    func testDecodesGatewayRoutes() throws {
        let net = try decode(
            #"""
            {"nodes":[
              {"id":"gr/shop/api","layer":2,"kind":"GRPCRoute","namespace":"shop","name":"api","detail":"","health":"ok","url":"","managed":true},
              {"id":"tr/home/mosquitto","layer":2,"kind":"TLSRoute","namespace":"home","name":"mosquitto","detail":"","health":"ok","url":"","managed":false}],
             "edges":[]}
            """#)
        XCTAssertEqual(net.nodes.map(\.kind), [.grpcRoute, .tlsRoute])
        XCTAssertTrue(net.nodes.allSatisfy { $0.kind.routesTraffic })
        XCTAssertTrue(net.nodes[1].shared)
    }

    func testProblemReasons() {
        XCTAssertEqual(NetMapProblem(kind: .node, name: "w3", detail: "NotReady").reason, .nodeNotReady("w3"))
        XCTAssertEqual(
            NetMapProblem(kind: .node, name: "w3", detail: "SchedulingDisabled").reason, .nodeCordoned("w3"))
        XCTAssertEqual(
            NetMapProblem(kind: .pod, name: "p", detail: "CrashLoopBackOff · 14 restarts").reason,
            .pod("p", "CrashLoopBackOff · 14 restarts"))
        XCTAssertEqual(NetMapProblem(kind: .service, name: "s").reason, .serviceWithoutPods("s"))
        XCTAssertEqual(NetMapProblem(kind: .ingress, name: "i").reason, .noHealthyBackend("Ingress", "i"))
        XCTAssertEqual(NetMapProblem(kind: .httpRoute, name: "r").reason, .noHealthyBackend("HTTPRoute", "r"))
        XCTAssertEqual(NetMapProblem(kind: .gateway, name: "g").reason, .noHealthyBackend("Gateway", "g"))
        XCTAssertEqual(NetMapProblem(kind: .ingressRoute, name: "t").reason, .noHealthyBackend("IngressRoute", "t"))
        XCTAssertEqual(NetMapProblem(kind: .unknown, name: "x", detail: "d").reason, .other("", "x", "d"))
    }
}

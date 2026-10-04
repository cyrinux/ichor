import XCTest
@testable import IchorCore

final class ArgoNetworkTests: XCTestCase {
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

    private func decode(_ text: String) throws -> ArgoNetwork {
        try JSONDecoder().decode(ArgoNetwork.self, from: Data(text.utf8))
    }

    func testDecodesTheDemoShape() throws {
        let net = try decode(json)
        XCTAssertEqual(net.nodes.count, 8)
        XCTAssertEqual(net.edges.count, 7)
        let gateway = try XCTUnwrap(net.node("gw/traefik/homelab"))
        XCTAssertEqual(gateway.layer, .gateway)
        XCTAssertEqual(gateway.kind, .gateway)
        XCTAssertTrue(gateway.shared)
        XCTAssertFalse(net.node("hr/demo/worker")?.shared ?? true)
        XCTAssertFalse(net.node("pod/demo/worker-6f4b8-pqrst")?.shared ?? true, "a pod is never drawn as shared")
        XCTAssertEqual(net.node("host/https://worker.homelab.lan")?.url, "https://worker.homelab.lan")
        XCTAssertEqual(net.node("svc/demo/worker")?.health, .warning)
        XCTAssertEqual(net.edges[3].health, .critical)
        XCTAssertEqual(net.problem, ArgoNetProblem(kind: .node, name: "demo-worker-3", detail: "NotReady"))
    }

    func testDecodesTolerantly() throws {
        let net = try decode(#"{"nodes":[{"id":"x","layer":9,"kind":"Mystery","name":"x"}],"problem":null}"#)
        XCTAssertEqual(net.nodes.first?.layer, .node, "an unknown layer is clamped")
        XCTAssertEqual(net.nodes.first?.kind, .unknown)
        XCTAssertEqual(net.nodes.first?.health, .unknown)
        XCTAssertTrue(net.edges.isEmpty)
        XCTAssertNil(net.problem)
        XCTAssertTrue(try decode("{}").isEmpty)
    }

    func testColumnsSkipEmptyLayers() throws {
        let net = try decode(json)
        XCTAssertEqual(net.columns().map(\.layer), [.entry, .gateway, .route, .service, .pod, .node])
        let noGateway = ArgoNetwork(nodes: net.nodes.filter { $0.kind != .gateway && $0.kind != .httpRoute })
        XCTAssertEqual(noGateway.columns().map(\.layer), [.entry, .service, .pod, .node])
        XCTAssertEqual(noGateway.columns()[2].nodes.first?.name, "worker-6f4b8-uvwxy", "worst first, as sent")
        XCTAssertTrue(ArgoNetwork().columns().isEmpty)
    }

    func testPodsBeyondTheLimitFold() {
        let pods = (0..<11).map { ArgoNetNode(id: "pod/ns/p\($0)", layer: .pod, kind: .pod, namespace: "ns", name: "p\($0)") }
        let node = ArgoNetNode(id: "node/n1", layer: .node, kind: .node, name: "n1")
        let svc = ArgoNetNode(id: "svc/ns/s", layer: .service, kind: .service, namespace: "ns", name: "s")
        let edges = pods.flatMap { [ArgoNetEdge(from: svc.id, to: $0.id), ArgoNetEdge(from: $0.id, to: node.id, health: $0.name == "p10" ? .critical : .ok)] }
        let net = ArgoNetwork(nodes: [svc] + pods + [node], edges: edges)

        let column = net.columns()[1]
        XCTAssertEqual(column.nodes.count, 8)
        XCTAssertEqual(column.hidden.map(\.name), ["p8", "p9", "p10"])
        XCTAssertTrue(net.columns(expanded: true)[1].hidden.isEmpty)

        let hidden = Set(column.hidden.map(\.id))
        let shown = net.edges(hiding: hidden)
        XCTAssertEqual(shown.count, 16 + 2, "the folded pods share one hop in, one hop out")
        XCTAssertEqual(shown.first { $0.from == ArgoNetwork.morePodsID }?.health, .critical, "the worst they stand for")

        let path = net.path(through: "pod/ns/p9", hiding: hidden)
        XCTAssertTrue(path.contains(node: ArgoNetwork.morePodsID))
        XCTAssertTrue(path.contains(edge: "svc/ns/s>\(ArgoNetwork.morePodsID)"))
        XCTAssertTrue(path.contains(edge: "\(ArgoNetwork.morePodsID)>node/n1"))
        XCTAssertFalse(path.contains(node: "pod/ns/p0"))
    }

    func testPathThroughAServiceLightsBothWays() throws {
        let path = try decode(json).path(through: "svc/demo/worker")
        XCTAssertEqual(path.nodes, ["host/https://worker.homelab.lan", "gw/traefik/homelab", "hr/demo/worker", "svc/demo/worker",
                                    "pod/demo/worker-6f4b8-uvwxy", "pod/demo/worker-6f4b8-pqrst", "node/demo-worker-3", "node/demo-worker-2"])
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
        XCTAssertEqual(path.nodes, ["node/demo-worker-2", "pod/demo/worker-6f4b8-pqrst", "svc/demo/worker", "hr/demo/worker",
                                    "gw/traefik/homelab", "host/https://worker.homelab.lan"])
    }

    func testTalosDownNodesTurnCritical() throws {
        let healthy = ArgoNetwork(nodes: try decode(json).nodes.filter { $0.name != "demo-worker-3" },
                                  edges: [ArgoNetEdge(from: "pod/demo/worker-6f4b8-pqrst", to: "node/demo-worker-2")],
                                  problem: ArgoNetProblem(kind: .pod, namespace: "demo", name: "worker-6f4b8-uvwxy", detail: "CrashLoopBackOff"))
        let marked = healthy.marking(down: ["demo-worker-2"])
        XCTAssertEqual(marked.node("node/demo-worker-2")?.health, .critical)
        XCTAssertEqual(marked.node("node/demo-worker-2")?.detail, "NotReady")
        XCTAssertEqual(marked.edges.first?.health, .critical)
        XCTAssertEqual(marked.problem?.reason, .nodeNotReady("demo-worker-2"), "the deepest cause wins")
        XCTAssertEqual(healthy.marking(down: ["elsewhere"]), healthy)
        XCTAssertEqual(try decode(json).marking(down: ["demo-worker-3"]), try decode(json), "already critical")
    }

    func testProblemReasons() {
        XCTAssertEqual(ArgoNetProblem(kind: .node, name: "w3", detail: "NotReady").reason, .nodeNotReady("w3"))
        XCTAssertEqual(ArgoNetProblem(kind: .node, name: "w3", detail: "SchedulingDisabled").reason, .nodeCordoned("w3"))
        XCTAssertEqual(ArgoNetProblem(kind: .pod, name: "p", detail: "CrashLoopBackOff · 14 restarts").reason,
                       .pod("p", "CrashLoopBackOff · 14 restarts"))
        XCTAssertEqual(ArgoNetProblem(kind: .service, name: "s").reason, .serviceWithoutPods("s"))
        XCTAssertEqual(ArgoNetProblem(kind: .ingress, name: "i").reason, .noHealthyBackend("Ingress", "i"))
        XCTAssertEqual(ArgoNetProblem(kind: .httpRoute, name: "r").reason, .noHealthyBackend("HTTPRoute", "r"))
        XCTAssertEqual(ArgoNetProblem(kind: .gateway, name: "g").reason, .noHealthyBackend("Gateway", "g"))
        XCTAssertEqual(ArgoNetProblem(kind: .unknown, name: "x", detail: "d").reason, .other("", "x", "d"))
    }
}

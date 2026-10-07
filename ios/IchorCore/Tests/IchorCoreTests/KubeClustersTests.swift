import XCTest
@testable import IchorCore

final class KubeClustersTests: XCTestCase {
    private let kubeJSON = """
    {"current":"prod","contexts":[
      {"name":"broken","kind":"kube","fingerprint":"k1","clusterId":"c1","endpoints":["http://x"],"nodes":[],"roles":[],
       "certNotAfter":0,"auth":"exec","problem":"kube-exec-unsupported","problemDetail":"some-plugin"},
      {"name":"prod","kind":"kube","fingerprint":"k2","clusterId":"c2","endpoints":["https://k8s.example:6443"],
       "nodes":null,"roles":null,"certNotAfter":1796385050,"namespace":"apps","auth":"token","user":"system:serviceaccount:apps:ichor"}
    ]}
    """

    func testDecodesKubeconfigSummary() throws {
        let summary = try TalosJSON.decode(ConfigSummary.self, from: kubeJSON)
        let prod = try XCTUnwrap(summary.context(named: "prod"))
        XCTAssertTrue(prod.isKube)
        XCTAssertEqual(prod.endpoints, ["https://k8s.example:6443"])
        XCTAssertEqual(prod.roles, [])
        XCTAssertEqual(prod.namespace, "apps")
        XCTAssertEqual(prod.auth, "token")
        XCTAssertEqual(prod.user, "system:serviceaccount:apps:ichor")
        XCTAssertNil(prod.problem)
        XCTAssertFalse(prod.insecure)
        XCTAssertEqual(summary.contexts[0].problem, "kube-exec-unsupported")
        XCTAssertEqual(summary.contexts[0].problemDetail, "some-plugin")
        XCTAssertEqual(summary.importableIndices, [1])
    }

    func testTalosContextWithoutKindIsTalos() throws {
        let json = #"{"current":"lab","contexts":[{"name":"lab","endpoints":["10.0.0.1"],"roles":["os:admin"]}]}"#
        let lab = try XCTUnwrap(TalosJSON.decode(ConfigSummary.self, from: json).contexts.first)
        XCTAssertEqual(lab.kind, ContextKind.talos)
        XCTAssertFalse(lab.isKube)
        XCTAssertNil(lab.auth)
    }

    func testKubeClustersAllowKubernetesFeaturesOnly() {
        let kube = ContextSummary(name: "prod", kind: ContextKind.kube)
        for feature in Feature.allCases {
            XCTAssertEqual(kube.allows(feature), feature == .workloads || feature == .kubeconfig, "\(feature)")
        }
        // Roles never apply to a kubeconfig cluster.
        XCTAssertFalse(ContextSummary(name: "odd", kind: ContextKind.kube, roles: ["os:admin"]).allows(.health))
        XCTAssertTrue(ContextSummary(name: "lab", roles: ["os:admin"]).allows(.health))
    }

    func testCombinedListsTalosThenKube() {
        let talos = ConfigSummary(current: "lab", contexts: [ContextSummary(name: "lab"), ContextSummary(name: "home")])
        let kube = ConfigSummary(current: "prod", contexts: [ContextSummary(name: "prod", kind: ContextKind.kube)])

        let both = ConfigSummary.combined(talos: talos, kube: kube)
        XCTAssertEqual(both?.contexts.map(\.name), ["lab", "home", "prod"])
        XCTAssertEqual(both?.current, "lab")
        XCTAssertEqual(both?.selectedContext(index: 2, name: nil), "prod")

        XCTAssertEqual(ConfigSummary.combined(talos: nil, kube: kube), kube)
        XCTAssertEqual(ConfigSummary.combined(talos: talos, kube: nil), talos)
        XCTAssertNil(ConfigSummary.combined(talos: nil, kube: nil))
        let emptyTalos = ConfigSummary(current: "", contexts: [])
        XCTAssertEqual(ConfigSummary.combined(talos: emptyTalos, kube: kube)?.current, "prod")
    }

    func testStoredConfigStatus() {
        XCTAssertEqual(StoredConfigStatus(stored: false, parsed: false), .absent)
        XCTAssertEqual(StoredConfigStatus(stored: true, parsed: true), .loaded)
        XCTAssertEqual(StoredConfigStatus(stored: true, parsed: false), .unreadable)

        XCTAssertEqual(unreadableConfigs([.talosconfig: .loaded, .kubeconfig: .absent]), [])
        XCTAssertEqual(unreadableConfigs([.talosconfig: .loaded, .kubeconfig: .unreadable]), [.kubeconfig])
        XCTAssertEqual(unreadableConfigs([.kubeconfig: .unreadable, .talosconfig: .unreadable]), [.talosconfig, .kubeconfig])
    }

    func testImportChoicesSkipAndReplace() throws {
        let choices = kubeImportChoices(count: 4, selected: [1, 2, 3], replacing: [2, 0])
        XCTAssertEqual(choices, [
            KubeImportChoice(index: 0, skip: true),
            KubeImportChoice(index: 2, replace: true),
        ])
        // Unset fields stay out, as Go's omitempty would.
        let json = try TalosJSON.encode(choices)
        let decoded = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(json.utf8)) as? [[String: Any]])
        XCTAssertEqual(decoded.map { $0.keys.sorted() }, [["index", "skip"], ["index", "replace"]])
        XCTAssertEqual(kubeImportChoices(count: 0, selected: [], replacing: []), [])
    }

    func testKubeNodesDecode() throws {
        let json = """
        {"serverVersion":"v1.31.2","nodes":[
          {"name":"cp-1","roles":["control-plane"],"ready":true,"cordoned":false,"internalIP":"10.0.0.2",
           "kubelet":"v1.31.2","cpu":4,"memory":8.0e9,"podLimit":110,"pressure":null,"created":1700000000},
          {"name":"w-1","roles":null,"ready":false,"cordoned":true,"externalIP":"203.0.113.4","cpu":2,"memory":4.0e9,
           "pool":"general","poolKind":"karpenter","instanceType":"m6i.large","capacity":"spot",
           "podLimit":110,"pressure":["DiskPressure"],"created":1700000001}
        ]}
        """
        let overview = try TalosJSON.decode(KubeNodesOverview.self, from: json)
        XCTAssertEqual(overview.serverVersion, "v1.31.2")
        XCTAssertFalse(overview.forbidden)
        XCTAssertEqual(overview.readyCount, 1)
        XCTAssertEqual(overview.nodes[0].address, "10.0.0.2")
        XCTAssertEqual(overview.nodes[0].pressure, [])
        XCTAssertFalse(overview.nodes[0].needsAttention)
        XCTAssertEqual(overview.nodes[1].roles, [])
        XCTAssertEqual(overview.nodes[1].address, "203.0.113.4")
        XCTAssertTrue(overview.nodes[1].needsAttention)
        XCTAssertNil(overview.nodes[0].pool)
        XCTAssertNil(overview.nodes[0].capacity)
        XCTAssertEqual(overview.nodes[1].pool, "general")
        XCTAssertEqual(overview.nodes[1].poolKind, "karpenter")
        XCTAssertEqual(overview.nodes[1].instanceType, "m6i.large")
        XCTAssertEqual(overview.nodes[1].capacity, "spot")

        let forbidden = try TalosJSON.decode(KubeNodesOverview.self, from: #"{"serverVersion":"","nodes":null,"forbidden":true}"#)
        XCTAssertTrue(forbidden.forbidden)
        XCTAssertEqual(forbidden.nodes, [])
    }
}

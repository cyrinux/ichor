import XCTest
@testable import IchorCore

final class NetPoliciesTests: XCTestCase {
    // KubeNetworkPolicies' demo answer (go/ichorgo/kube_netpol_demo.go), allow-dns' pods cut to three.
    private let json = #"""
    {"cilium":true,"policies":[{"kind":"NetworkPolicy","namespace":"media","name":"immich-postgres-ingress","created":1789301677162,"subject":"app=immich-postgres","ingress":true,"egress":false,"ingressRules":[{"peers":[{"kind":"pods","selector":"app=immich-server"},
      {"kind":"pods","selector":"app=immich-machine-learning"}],"ports":[{"protocol":"TCP","port":"5432"}]}],"egressRules":[],"pods":["media/immich-postgres-1"],"podCount":1},
      {"kind":"NetworkPolicy","namespace":"home","name":"default-deny-egress","created":1789305277162,"subject":"","ingress":false,"egress":true,"ingressRules":[],"egressRules":[],"pods":["home/home-assistant-0","home/zigbee2mqtt-0","home/mosquitto-0"],"podCount":3},
      {"kind":"NetworkPolicy","namespace":"media","name":"allow-prometheus-scrape","created":1789308877162,"subject":"","ingress":true,"egress":false,"ingressRules":[{"peers":[{"kind":"pods","namespaceSelector":"kubernetes.io/metadata.name=monitoring","selector":"app=prometheus-kube-prometheus"}],"ports":[{"protocol":"TCP","port":"metrics"}]}],"egressRules":[],"pods":["media/jellyfin-5f8c-xq2wz","media/immich-server-7d9f-k2m8p","media/immich-machine-learning-6c4-p8x2n","media/immich-postgres-1","media/sonarr-0","media/radarr-0"],"podCount":6},
      {"kind":"CiliumNetworkPolicy","namespace":"home","name":"mosquitto","created":1789312477162,"subject":"app=mosquitto","description":"MQTT only from the home automation apps","ingress":true,"egress":false,"ingressRules":[{"peers":[{"kind":"pods","selector":"app=zigbee2mqtt"},
      {"kind":"pods","selector":"app=home-assistant"}],"ports":[{"protocol":"TCP","port":"1883"}]}],"egressRules":[],"pods":["home/mosquitto-0"],"podCount":1},
      {"kind":"CiliumNetworkPolicy","namespace":"home","name":"home-egress","created":1789316077162,"subject":"app=home-assistant","ingress":false,"egress":true,"ingressRules":[],"egressRules":[{"peers":[{"kind":"pods","selector":"app=mosquitto"}],"ports":[{"protocol":"TCP","port":"1883"}]},{"peers":[{"kind":"fqdn","value":"*.home-assistant.io"}],"ports":[{"protocol":"TCP","port":"443"}]},{"deny":true,"peers":[{"kind":"entity","value":"world"}],"ports":[{"protocol":"TCP","port":"25"}]}],"pods":["home/home-assistant-0"],"podCount":1},
      {"kind":"CiliumNetworkPolicy","namespace":"default","name":"vaultwarden-web","created":1789319677162,"subject":"app=vaultwarden","ingress":true,"egress":false,"ingressRules":[{"peers":[{"kind":"pods","namespace":"networking","selector":"app=traefik"}],"ports":[{"protocol":"TCP","port":"8080"}],"l7":["HTTP GET","HTTP POST /api/.*"]}],"egressRules":[],"pods":["default/vaultwarden-0"],"podCount":1},
      {"kind":"CiliumClusterwideNetworkPolicy","name":"allow-dns","created":1789323277162,"subject":"","description":"Every pod may resolve names","ingress":false,"egress":false,"ingressRules":[],"egressRules":[{"peers":[{"kind":"pods","namespace":"kube-system","selector":"k8s-app=kube-dns"}],"ports":[{"protocol":"ANY","port":"53"}],"l7":["DNS *"]}],"pods":["cert-manager/cert-manager-7b5d-9kq2w","cert-manager/cert-manager-cainjector-5c9-tt7x2","longhorn-system/longhorn-manager-r6k2p"],"podCount":27},
      {"kind":"CiliumClusterwideNetworkPolicy","name":"control-plane-host","created":1789326877162,"subject":"node-role.kubernetes.io/control-plane=","nodes":true,"ingress":true,"egress":false,"ingressRules":[{"peers":[{"kind":"entity","value":"cluster"}],"ports":[]},{"peers":[{"kind":"entity","value":"world"}],"ports":[{"protocol":"TCP","port":"6443"},{"protocol":"TCP","port":"50000"}]}],"egressRules":[],"pods":[],"podCount":0}],
     "namespaces":[
      {"namespace":"argocd","pods":2,"ingressIsolated":0,"egressIsolated":0,"policies":0},
      {"namespace":"cert-manager","pods":2,"ingressIsolated":0,"egressIsolated":0,"policies":0},
      {"namespace":"default","pods":3,"ingressIsolated":1,"egressIsolated":0,"policies":1},
      {"namespace":"demo","pods":1,"ingressIsolated":0,"egressIsolated":0,"policies":0},
      {"namespace":"flux-system","pods":2,"ingressIsolated":0,"egressIsolated":0,"policies":0},
      {"namespace":"home","pods":3,"ingressIsolated":1,"egressIsolated":3,"policies":3},
      {"namespace":"longhorn-system","pods":3,"ingressIsolated":0,"egressIsolated":0,"policies":0},
      {"namespace":"media","pods":6,"ingressIsolated":6,"egressIsolated":0,"policies":2},
      {"namespace":"monitoring","pods":3,"ingressIsolated":0,"egressIsolated":0,"policies":0},
      {"namespace":"networking","pods":1,"ingressIsolated":0,"egressIsolated":0,"policies":0},
      {"namespace":"tools","pods":1,"ingressIsolated":0,"egressIsolated":0,"policies":0}]}
    """#

    private func decode(_ text: String) throws -> NetPolicyReport {
        try JSONDecoder().decode(NetPolicyReport.self, from: Data(text.utf8))
    }

    func testDecodesTheDemo() throws {
        let report = try decode(json)
        XCTAssertTrue(report.cilium)
        XCTAssertEqual(report.policies.count, 8)
        XCTAssertEqual(report.namespaces.count, 11)
        XCTAssertEqual(report.error, "")

        let egress = try XCTUnwrap(report.policies.first { $0.name == "home-egress" })
        XCTAssertEqual(egress.kind, .cilium)
        XCTAssertEqual(egress.kind.short, "CNP")
        XCTAssertEqual(egress.subjectScope, .selector("app=home-assistant"))
        XCTAssertEqual(egress.egressRules.count, 3)
        XCTAssertEqual(egress.egressRules[1].peers, [NetPeer(kind: .fqdn, value: "*.home-assistant.io")])
        XCTAssertTrue(egress.egressRules[2].deny)
        XCTAssertEqual(egress.egressRules[2].peers.first?.kind, .entity)
        XCTAssertEqual(egress.effect(.egress), .isolated)
        XCTAssertEqual(egress.effect(.ingress), .open)

        let vault = try XCTUnwrap(report.policies.first { $0.name == "vaultwarden-web" })
        XCTAssertEqual(vault.ingressRules.first?.l7, ["HTTP GET", "HTTP POST /api/.*"])

        let dns = try XCTUnwrap(report.policies.first { $0.name == "allow-dns" })
        XCTAssertTrue(dns.clusterWide)
        XCTAssertEqual(dns.kind.short, "CCNP")
        XCTAssertEqual(dns.subjectScope, .allPodsOfCluster)
        XCTAssertEqual(dns.podCount, 27)
        XCTAssertEqual(dns.pods.count, 3)
        XCTAssertEqual(dns.effect(.egress), .rulesOnly, "allows without isolating")

        let host = try XCTUnwrap(report.policies.first { $0.name == "control-plane-host" })
        XCTAssertEqual(host.subjectScope, .nodes("node-role.kubernetes.io/control-plane="))
        XCTAssertTrue(host.ingressRules[0].ports.isEmpty, "no ports: any port")
    }

    func testDenyAllAndAllPods() throws {
        let report = try decode(json)
        let deny = try XCTUnwrap(report.policies.first { $0.name == "default-deny-egress" })
        XCTAssertEqual(deny.effect(.egress), .denyAll)
        XCTAssertEqual(deny.subjectScope, .allPods)
        XCTAssertEqual(deny.pods, ["home/home-assistant-0", "home/zigbee2mqtt-0", "home/mosquitto-0"])
    }

    func testNamespaceIsolation() throws {
        let rows = Dictionary(uniqueKeysWithValues: try decode(json).namespaces.map { ($0.namespace, $0) })
        XCTAssertEqual(rows["media"]?.isolation(.ingress), .full)
        XCTAssertEqual(rows["home"]?.isolation(.ingress), .partial)
        XCTAssertEqual(rows["home"]?.isolation(.egress), .full)
        XCTAssertEqual(rows["argocd"]?.isolation(.ingress), .open)
        XCTAssertEqual(NetPolicyNamespace(namespace: "empty", pods: 0).isolation(.ingress), .open)
    }

    func testPeerScopes() {
        let own = NetPeer(kind: .pods, selector: "app=a")
        XCTAssertEqual(own.scope(clusterWide: false), .own)
        XCTAssertEqual(own.scope(clusterWide: true), .any, "a cluster-wide policy's peer is in any namespace")
        XCTAssertEqual(NetPeer(kind: .pods, namespace: "*").scope(clusterWide: false), .any)
        XCTAssertEqual(NetPeer(kind: .pods, namespace: "networking").scope(clusterWide: false), .named("networking"))
        XCTAssertEqual(NetPeer(kind: .pods, namespace: "x", namespaceSelector: "team=a").scope(clusterWide: false), .matching("team=a"))
        XCTAssertEqual(NetPeer(kind: .service, value: "default/web").serviceName, "default/web")
        XCTAssertEqual(NetPeer(kind: .service, namespace: "default", selector: "app=web").serviceName, "default app=web")
        XCTAssertEqual(NetPeerKind(wire: "mystery"), .unknown)
    }

    func testPortLabels() {
        XCTAssertEqual(NetPort(protocol: "TCP", port: "5432").label, "TCP 5432")
        XCTAssertEqual(NetPort(protocol: "UDP", port: "8000", endPort: 8100).label, "UDP 8000–8100")
        XCTAssertEqual(NetPort(protocol: "ANY", port: "53").label, "53")
        XCTAssertEqual(NetPort(protocol: "TCP").label, "TCP")
        XCTAssertEqual(NetPort(protocol: "ANY").label, "", "any port, worded by the app")
        XCTAssertEqual(NetPort(protocol: "TCP", port: "http").label, "TCP http")
    }

    func testSectionsGroupByNamespaceClusterWideLast() throws {
        let report = try decode(json)
        let sections = report.sections(namespace: nil, query: "")
        XCTAssertEqual(sections.map(\.namespace), ["default", "home", "media", nil])
        XCTAssertEqual(sections[1].policies.map(\.name), ["default-deny-egress", "home-egress", "mosquitto"])
        XCTAssertEqual(sections.last?.policies.map(\.name), ["allow-dns", "control-plane-host"])

        let media = report.sections(namespace: "media", query: "")
        XCTAssertEqual(media.map(\.namespace), ["media", nil], "cluster-wide policies may apply there too")
        XCTAssertEqual(report.sections(namespace: nil, query: "MQTT").flatMap(\.policies).map(\.name), ["mosquitto"],
                       "matches the description")
        XCTAssertEqual(report.sections(namespace: nil, query: "ccnp").flatMap(\.policies).count, 2)
        XCTAssertEqual(report.policyNamespaces, ["default", "home", "media"])
        XCTAssertEqual(report.kindCounts.map(\.count), [3, 3, 2])
    }

    func testFindsAPolicyByRef() throws {
        let report = try decode(json)
        XCTAssertEqual(report.policy(NetPolicyRef(kind: "NetworkPolicy", namespace: "media", name: "immich-postgres-ingress"))?.podCount, 1)
        XCTAssertEqual(report.policy(NetPolicyRef(kind: "CiliumClusterwideNetworkPolicy", name: "allow-dns"))?.name, "allow-dns")
        XCTAssertNil(report.policy(NetPolicyRef(kind: "CiliumNetworkPolicy", namespace: "media", name: "immich-postgres-ingress")),
                     "the kind is part of the key")
    }

    func testDecodesTolerantly() throws {
        let report = try decode(#"{"policies":[{"kind":"Mystery","name":"x"}],"error":"CiliumNetworkPolicy: forbidden"}"#)
        XCTAssertFalse(report.cilium)
        XCTAssertEqual(report.policies.first?.kind, .unknown)
        XCTAssertEqual(report.policies.first?.effect(.ingress), .open)
        XCTAssertEqual(report.error, "CiliumNetworkPolicy: forbidden")
        XCTAssertTrue(try decode("{}").policies.isEmpty)
    }

    // KubeNetworkPolicies on a Calico cluster (go/ichorgo/kube_netpol_calico.go).
    func testDecodesCalicoPolicies() throws {
        let report = try decode(#"""
        {"calico":true,"policies":[
         {"kind":"NetworkPolicy.projectcalico.org","namespace":"shop","name":"default.api-egress","subject":"app == 'api'","tier":"default","order":100,"ingress":false,"egress":true,"ingressRules":[],
          "egressRules":[{"peers":[{"kind":"pods","selector":"app == 'db'"}],"ports":[{"protocol":"TCP","port":"5432"}]},
                         {"deny":true,"peers":[{"kind":"cidr","value":"0.0.0.0/0"},{"kind":"other","value":"not 10.0.0.0/8"}],"ports":[{"protocol":"TCP","port":"8000","endPort":8100}]},
                         {"action":"pass","peers":[{"kind":"service","value":"shop/frontend"}],"ports":[]},
                         {"action":"log","peers":[],"ports":[]}],"pods":["shop/api-1"],"podCount":1},
         {"kind":"GlobalNetworkPolicy","name":"default.team-a","subject":"all()","subjectNamespace":"team-a-web","order":10.5,"ingress":true,"egress":false,
          "ingressRules":[{"peers":[{"kind":"pods","namespace":"team-a-web"}],"ports":[]}],"egressRules":[],"pods":["team-a-web/web-1"],"podCount":1}
        ],"namespaces":[{"namespace":"shop","pods":3,"ingressIsolated":0,"egressIsolated":1,"policies":1}]}
        """#)
        XCTAssertTrue(report.calico)
        XCTAssertFalse(report.cilium)

        let api = try XCTUnwrap(report.policies.first { $0.name == "default.api-egress" })
        XCTAssertEqual(api.kind, .calico)
        XCTAssertEqual(api.kind.short, "Calico NP")
        XCTAssertEqual(api.subjectScope, .selector("app == 'api'"))
        XCTAssertEqual(api.egressRules.map(\.action), [.allow, .deny, .pass, .log])
        XCTAssertEqual(api.egressRules[1].peers[1].kind, .unknown, "a negation the app only words")
        XCTAssertEqual(api.egressRules[1].peers[1].value, "not 10.0.0.0/8")
        XCTAssertEqual(api.tier, "default")
        XCTAssertEqual(api.orderText, "100")
        XCTAssertEqual(api.egressRules[1].ports.first?.label, "TCP 8000–8100")
        XCTAssertEqual(api.effect(.egress), .isolated)

        let team = try XCTUnwrap(report.policies.first { $0.name == "default.team-a" })
        XCTAssertEqual(team.kind, .calicoGlobal)
        XCTAssertEqual(team.kind.short, "GNP")
        XCTAssertTrue(team.clusterWide)
        XCTAssertEqual(team.subjectNamespace, "team-a-web")
        XCTAssertEqual(team.subjectScope, .selector("all()"))
        XCTAssertEqual(team.ingressRules[0].peers[0].scope(clusterWide: true), .named("team-a-web"))
        XCTAssertEqual(team.orderText, "10.5")
        XCTAssertEqual(report.kindCounts.map(\.kind), [.calico, .calicoGlobal])
        // "calico" finds both of Calico's kinds, "gnp" the global ones.
        XCTAssertEqual(report.sections(namespace: nil, query: "calico").flatMap(\.policies).map(\.name), ["default.api-egress", "default.team-a"])
        XCTAssertEqual(report.sections(namespace: nil, query: "gnp").flatMap(\.policies).map(\.name), ["default.team-a"])
    }
}

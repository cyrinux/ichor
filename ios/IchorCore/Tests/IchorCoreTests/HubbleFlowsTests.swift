import XCTest
@testable import IchorCore

final class HubbleFlowsTests: XCTestCase {
    // A snapshot of StartHubbleFlows on the demo cluster (go/ichorgo/kube_hubble_demo.go), cut to
    // two flows and three drop groups.
    private let json = #"""
    {"namespace":"kube-system","version":"v1.18.2","buffer":4095,
     "nodes":[{"node":"demo-cp-1","pod":"cilium-ah2kd","state":"connecting","flows":0},{"node":"demo-cp-2","pod":"cilium-bi2kd","state":"connecting","flows":0},{"node":"demo-cp-3","pod":"cilium-cj2kd","state":"connecting","flows":0},{"node":"demo-worker-1","pod":"cilium-dk2kd","state":"live","flows":70},{"node":"demo-worker-2","pod":"cilium-el2kd","state":"live","flows":50}],
     "flows":[
      {"time":1791116075956,"node":"demo-worker-1","verdict":"FORWARDED","direction":"INGRESS","protocol":"TCP","port":5432,"flags":"SYN","type":"L3_L4","source":{"namespace":"media","pod":"immich-server-7d9f-k2m8p","workload":"immich-server-7d9f","identity":11811,"ip":"10.244.5.82"},"destination":{"namespace":"media","pod":"immich-postgres-1","workload":"immich-postgres","identity":11520,"ip":"10.244.5.61"}},{"time":1791116075090,"node":"demo-worker-1","verdict":"FORWARDED","direction":"INGRESS","protocol":"TCP","port":8080,"flags":"SYN","type":"L7","l7":"HTTP GET /api/sync → 200","source":{"namespace":"networking","pod":"traefik-8c6d-w7r2m","workload":"traefik-8c6d","identity":11294,"ip":"10.244.2.64"},"destination":{"namespace":"default","pod":"vaultwarden-0","workload":"vaultwarden","identity":11158,"ip":"10.244.7.49"}}],
     "drops":[
      {"source":{"namespace":"home","pod":"home-assistant-0","workload":"home-assistant","identity":11410,"ip":"10.244.4.58"},"destination":{"identity":2,"ip":"198.51.100.25","names":["smtp.example.net"],"reserved":"world"},"protocol":"TCP","port":25,"direction":"EGRESS","verdict":"DROPPED","reason":"POLICY_DENY","count":9,"firstSeen":1791116032689,"lastSeen":1791116073859,"nodes":["demo-worker-2"],"deniedBy":[{"kind":"CiliumNetworkPolicy","namespace":"home","name":"home-egress"}],"isolating":[],"sample":{"time":1791116073859,"node":"demo-worker-2","verdict":"DROPPED","reason":"POLICY_DENY","direction":"EGRESS","protocol":"TCP","port":25,"flags":"SYN","type":"L3_L4","source":{"namespace":"home","pod":"home-assistant-0","workload":"home-assistant","identity":11410,"ip":"10.244.4.58"},"destination":{"identity":2,"ip":"198.51.100.25","names":["smtp.example.net"],"reserved":"world"},"deniedBy":[{"kind":"CiliumNetworkPolicy","namespace":"home","name":"home-egress"}]}},
      {"source":{"namespace":"media","pod":"jellyfin-5f8c-xq2wz","workload":"jellyfin-5f8c","identity":11326,"ip":"10.244.5.67"},"destination":{"namespace":"media","pod":"immich-postgres-1","workload":"immich-postgres","identity":11520,"ip":"10.244.5.61"},"protocol":"TCP","port":5432,"direction":"INGRESS","verdict":"DROPPED","reason":"POLICY_DENIED","count":10,"firstSeen":1791116033921,"lastSeen":1791116068781,"nodes":["demo-worker-1"],"deniedBy":[],"isolating":[{"kind":"NetworkPolicy","namespace":"media","name":"immich-postgres-ingress"},{"kind":"NetworkPolicy","namespace":"media","name":"allow-prometheus-scrape"}],"sample":{"time":1791116068781,"node":"demo-worker-1","verdict":"DROPPED","reason":"POLICY_DENIED","direction":"INGRESS","protocol":"TCP","port":5432,"flags":"SYN","type":"L3_L4","source":{"namespace":"media","pod":"jellyfin-5f8c-xq2wz","workload":"jellyfin-5f8c","identity":11326,"ip":"10.244.5.67"},"destination":{"namespace":"media","pod":"immich-postgres-1","workload":"immich-postgres","identity":11520,"ip":"10.244.5.61"}}},
      {"source":{"namespace":"home","pod":"zigbee2mqtt-0","workload":"zigbee2mqtt","identity":11119,"ip":"10.244.4.49"},"destination":{"identity":2,"ip":"198.51.100.80","names":["ota.example.org"],"reserved":"world"},"protocol":"TCP","port":443,"direction":"EGRESS","verdict":"DROPPED","reason":"POLICY_DENIED","count":6,"firstSeen":1791116025871,"lastSeen":1791116065830,"nodes":["demo-worker-1"],"deniedBy":[],"isolating":[{"kind":"NetworkPolicy","namespace":"home","name":"default-deny-egress"}],"sample":{"time":1791116065830,"node":"demo-worker-1","verdict":"DROPPED","reason":"POLICY_DENIED","direction":"EGRESS","protocol":"TCP","port":443,"flags":"SYN","type":"L3_L4","source":{"namespace":"home","pod":"zigbee2mqtt-0","workload":"zigbee2mqtt","identity":11119,"ip":"10.244.4.49"},"destination":{"identity":2,"ip":"198.51.100.80","names":["ota.example.org"],"reserved":"world"}}}],"seen":120,"dropped":29,"lost":0}
    """#

    // KubeCilium's demo answer.
    private let cilium = #"""
    {"installed":true,"namespace":"kube-system","version":"v1.18.2","hubble":true,"buffer":4095,"agents":[{"node":"demo-cp-1","pod":"cilium-ah2kd","ready":true},{"node":"demo-cp-2","pod":"cilium-bi2kd","ready":true},{"node":"demo-cp-3","pod":"cilium-cj2kd","ready":true},{"node":"demo-worker-1","pod":"cilium-dk2kd","ready":true},{"node":"demo-worker-2","pod":"cilium-el2kd","ready":true}]}
    """#

    private func decode(_ text: String) throws -> HubbleSnapshot {
        try JSONDecoder().decode(HubbleSnapshot.self, from: Data(text.utf8))
    }

    func testDecodesCiliumStatus() throws {
        let status = try JSONDecoder().decode(CiliumStatus.self, from: Data(cilium.utf8))
        XCTAssertTrue(status.installed)
        XCTAssertTrue(status.hubble)
        XCTAssertEqual(status.version, "v1.18.2")
        XCTAssertEqual(status.buffer, 4095)
        XCTAssertEqual(status.agents.count, 5)
        XCTAssertEqual(status.agents.first?.node, "demo-cp-1")
        let absent = try JSONDecoder().decode(CiliumStatus.self, from: Data(#"{"installed":false,"hubble":false,"agents":[]}"#.utf8))
        XCTAssertFalse(absent.installed)
    }

    func testDecodesTheDemoSnapshot() throws {
        let snap = try decode(json)
        XCTAssertEqual(snap.seen, 120)
        XCTAssertEqual(snap.dropped, 29)
        XCTAssertEqual(snap.buffer, 4095)
        XCTAssertEqual(snap.nodes.count, 5)
        XCTAssertEqual(snap.nodes[3].state, .live)
        XCTAssertEqual(snap.nodes[3].flows, 70)
        XCTAssertEqual(snap.agentsByAttention.map(\.state), [.connecting, .connecting, .connecting, .live, .live])

        let flow = try XCTUnwrap(snap.flows.first)
        XCTAssertEqual(flow.verdict, .forwarded)
        XCTAssertEqual(flow.direction, .ingress)
        XCTAssertEqual(flow.portLabel, "TCP 5432")
        XCTAssertEqual(flow.source.label, "media/immich-server-7d9f-k2m8p")
        XCTAssertEqual(snap.flows[1].l7, "HTTP GET /api/sync → 200")
        XCTAssertEqual(snap.flowNamespaces, ["default", "home", "media", "networking"])
    }

    func testAgentsInErrorComeFirst() {
        let snap = HubbleSnapshot(nodes: [HubbleAgentState(node: "a", state: .live), HubbleAgentState(node: "b", state: .error, error: "exec refused"),
                                          HubbleAgentState(node: "c", state: .connecting)])
        XCTAssertEqual(snap.agentsByAttention.map(\.node), ["b", "c", "a"])
    }

    func testDropGroups() throws {
        let drops = try decode(json).drops
        XCTAssertEqual(drops.count, 3)
        XCTAssertEqual(Set(drops.map(\.id)).count, 3)

        let smtp = drops[0]
        XCTAssertEqual(smtp.reason, .policyDeny)
        XCTAssertEqual(smtp.verdict, .dropped)
        XCTAssertEqual(smtp.count, 9)
        XCTAssertEqual(smtp.destination.label, "smtp.example.net")
        XCTAssertEqual(smtp.destination.reserved, "world")
        XCTAssertEqual(smtp.endpointSuffix, ":25/TCP")
        XCTAssertEqual(smtp.deniedBy, [NetPolicyRef(kind: "CiliumNetworkPolicy", namespace: "home", name: "home-egress")])
        XCTAssertEqual(smtp.hint, .removeDeny(policies: smtp.deniedBy))

        let postgres = drops[1]
        XCTAssertEqual(postgres.reason, .policyDenied)
        XCTAssertEqual(postgres.direction, .ingress)
        XCTAssertEqual(postgres.isolating.map(\.name), ["immich-postgres-ingress", "allow-prometheus-scrape"])
        XCTAssertEqual(postgres.hint, .allowIngress(from: "media/jellyfin-5f8c", port: "TCP 5432", policies: postgres.isolating))
        XCTAssertEqual(postgres.sample.verdict, .dropped)

        let ota = drops[2]
        XCTAssertEqual(ota.hint, .allowEgress(to: "ota.example.org", port: "TCP 443", policies: ota.isolating))
    }

    func testNoHintWithoutPolicies() {
        let group = HubbleDropGroup(source: HubblePeer(namespace: "a", pod: "p"), destination: HubblePeer(ip: "10.0.0.1"),
                                    protocol: "UDP", port: 53, direction: "EGRESS", reason: "STALE_OR_UNROUTABLE_IP")
        XCTAssertNil(group.hint)
        XCTAssertEqual(group.reason, .staleOrUnroutableIP)
        XCTAssertEqual(group.destination.label, "10.0.0.1")
        XCTAssertEqual(group.endpointSuffix, ":53/UDP")
    }

    func testDropReasons() {
        XCTAssertEqual(DropReason(wire: "AUTH_REQUIRED"), .authRequired)
        XCTAssertEqual(DropReason(wire: "CT_MAP_INSERTION_FAILED"), .ctMapInsertionFailed)
        XCTAssertEqual(DropReason(wire: ""), .unspecified)
        XCTAssertEqual(DropReason(wire: "UNSUPPORTED_L3_PROTOCOL"), .other("Unsupported L3 protocol"))
        XCTAssertEqual(DropReason(wire: "INVALID_SOURCE_IP"), .other("Invalid source IP"))
        XCTAssertEqual(DropReason(wire: "181"), .other("181"))
        XCTAssertEqual(HubbleVerdict(wire: "AUDIT"), .audit)
        XCTAssertEqual(HubbleVerdict(wire: "TRACED"), .other)
    }

    func testPeerLabels() {
        XCTAssertEqual(HubblePeer(reserved: "host").label, "host")
        XCTAssertEqual(HubblePeer(ip: "198.51.100.1", reserved: "world").label, "198.51.100.1")
        XCTAssertEqual(HubblePeer(namespace: "media", pod: "web-1", workload: "web").ruleLabel, "media/web")
        XCTAssertEqual(HubblePeer().label, "?")
    }

    func testSampleJSONLeavesEmptyFieldsOut() throws {
        let sample = try decode(json).drops[1].sample
        let text = sample.json
        XCTAssertTrue(text.contains(#""reason" : "POLICY_DENIED""#), text)
        XCTAssertFalse(text.contains("deniedBy"), "no attribution: left out")
        XCTAssertFalse(text.contains(#""names""#), "no DNS names: left out")
        let back = try JSONDecoder().decode(HubbleFlow.self, from: Data(text.utf8))
        XCTAssertEqual(back, sample)
    }

    func testFilter() {
        XCTAssertTrue(HubbleFilter().wire == ("", ""))
        XCTAssertNil(HubbleFilter(pod: "p").pod, "a pod needs its namespace")
        XCTAssertTrue(HubbleFilter(namespace: "media", pod: "web-1").wire == ("media", "web-1"))
        XCTAssertNotEqual(HubbleFilter(dropsOnly: true), HubbleFilter())
    }

    func testDecodesTolerantly() throws {
        let snap = try decode(#"{"nodes":[{"node":"n","state":"weird"}],"drops":[{"source":{},"destination":{}}]}"#)
        XCTAssertEqual(snap.nodes.first?.state, .connecting)
        XCTAssertEqual(snap.drops.first?.sample.verdict, .other)
        XCTAssertTrue(try decode("{}").flows.isEmpty)
    }
}

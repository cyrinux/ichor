import XCTest
@testable import TalosViewerCore

final class ModelsTests: XCTestCase {
    func testConfigSummaryToleratesNullSlices() throws {
        // Go encodes a context without nodes as "nodes": null.
        let json = #"{"current":"lab","contexts":[{"name":"lab","endpoints":["10.0.0.1"],"nodes":null,"roles":["os:reader"],"certNotAfter":1796385050}]}"#
        let summary = try TalosJSON.decode(ConfigSummary.self, from: json)
        XCTAssertEqual(summary.current, "lab")
        XCTAssertEqual(summary.contexts[0].nodes, [])
        XCTAssertEqual(summary.context(named: "lab")?.certNotAfter, 1_796_385_050)
    }

    func testOverviewHealthAndOptionalError() throws {
        let json = """
        {"context":"lab","nodes":[
          {"node":"10.0.0.2","hostname":"cp-1","reachable":true,"version":"v1.14.1","arch":"amd64",
           "platform":"metal","role":"controlplane","stage":"running","ready":true,"unmetConditions":[]},
          {"node":"10.0.0.3","hostname":"cp-2","reachable":true,"version":"v1.14.1","arch":"amd64",
           "platform":"metal","role":"controlplane","stage":"running","ready":false,
           "unmetConditions":[{"name":"services","reason":"etcd not healthy"}]},
          {"node":"10.0.0.9","hostname":"10.0.0.9","reachable":false,"error":"timed out","version":"",
           "arch":"","platform":"","role":"unknown","stage":"unknown","ready":false,"unmetConditions":[],"future":1}
        ]}
        """
        let overview = try TalosJSON.decode(ClusterOverview.self, from: json)
        XCTAssertEqual(overview.nodes.map(\.health), [.ready, .notReady, .unreachable])
        XCTAssertNil(overview.nodes[0].error)
        XCTAssertEqual(overview.nodes[2].error, "timed out")
    }

    func testServicesWithOmittedFields() throws {
        let json = #"[{"id":"apid","state":"Running","health":"unknown"},{"id":"etcd","state":"Running","health":"unhealthy","message":"context deadline exceeded","lastChange":1700000000}]"#
        let services = try TalosJSON.decode([ServiceInfo].self, from: json)
        XCTAssertNil(services[0].message)
        XCTAssertEqual(services[1].lastChange, 1_700_000_000)
    }

    func testResourcesAndEtcdAndLogs() throws {
        let resources = try TalosJSON.decode(NodeResources.self, from: #"{"memTotal":33347887104,"memAvailable":23890718720,"load1":0.83,"load5":1.24,"load15":1.35,"bootTime":1790699235,"cpuCount":8,"cpuModel":"i7","mounts":[{"filesystem":"/dev/dm-1","mountedOn":"/var","size":509534134272,"available":88529825792}]}"#)
        XCTAssertEqual(resources.mounts.first?.id, "/var")

        let etcd = try TalosJSON.decode(EtcdOverview.self, from: #"{"leaderId":"8e9e05c52164694d","members":[{"id":"8e9e05c52164694d","hostname":"cp-1","peerUrls":["https://10.0.0.2:2380"],"clientUrls":[],"isLearner":false}],"statuses":[{"node":"10.0.0.2","memberId":"8e9e05c52164694d","isLeader":true,"isLearner":false,"dbSize":467779584,"dbSizeInUse":42336256,"raftIndex":358583460,"raftTerm":856,"version":"3.7.0","errors":[]}],"alarms":[]}"#)
        XCTAssertTrue(etcd.statuses[0].isLeader)
        XCTAssertNil(etcd.error)

        let logs = try TalosJSON.decode(LogTail.self, from: #"{"lines":["a","b"],"truncated":true}"#)
        XCTAssertEqual(logs.lines.count, 2)
    }

    func testFeatureRoles() {
        let reader = ContextSummary(name: "r", roles: ["os:reader"])
        let operatorCtx = ContextSummary(name: "o", roles: ["os:operator"])
        let admin = ContextSummary(name: "a", roles: ["os:admin"])
        XCTAssertTrue(Feature.allCases.allSatisfy { !reader.allows($0) })
        XCTAssertTrue(operatorCtx.allows(.power))
        XCTAssertFalse(operatorCtx.allows(.health))
        XCTAssertTrue(Feature.allCases.allSatisfy { admin.allows($0) })
        XCTAssertEqual(Feature.power.minimumRole, "os:operator")
        XCTAssertEqual(Feature.kubeconfig.minimumRole, "os:admin")
    }

    func testKubeSpan() throws {
        let json = #"{"nodes":[{"node":"10.0.0.2","enabled":true,"up":1,"down":0,"peers":[{"publicKey":"pk","label":"cp-2","state":"up","endpoint":"10.0.0.3:51820","rx":10,"tx":20,"lastHandshake":1800000000}]},{"node":"10.0.0.9","error":"timed out","enabled":false,"up":0,"down":0,"peers":[]}]}"#
        let ks = try TalosJSON.decode(KubeSpanOverview.self, from: json)
        XCTAssertEqual(ks.nodes[0].peers[0].state, "up")
        XCTAssertEqual(ks.nodes[1].error, "timed out")
    }
}

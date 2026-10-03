import XCTest
@testable import IchorCore

final class DragonflyTests: XCTestCase {
    private let json = #"""
    {"longhorn":{"nodes":[{"name":"node-3","ready":false}]},
     "dragonfly":{"version":"v1alpha1","error":"","instances":[
      {"namespace":"app","name":"down","phase":"Ready","health":"critical","reasons":["noReady"],"replicas":2,"readyPods":0,"master":"down-0",
       "pods":[{"name":"down-0","node":"node-3","phase":"Running","role":"master","ready":false},{"name":"down-1","node":"","phase":"Pending","role":"replica","ready":false}]},
      {"namespace":"app","name":"queue","phase":"Ready","health":"warning","reasons":["pods"],"replicas":2,"readyPods":1,"master":"queue-0",
       "pods":[{"name":"queue-0","node":"node-1","phase":"Running","role":"master","ready":true},{"name":"queue-1","node":"","phase":"Pending","role":"replica","ready":false}]},
      {"namespace":"app","name":"rolling","phase":"Rolling-update","health":"warning","reasons":["notReady"],"replicas":1,"readyPods":1,"master":"rolling-0",
       "pods":[{"name":"rolling-0","node":"node-1","phase":"Running","role":"master","ready":true}]},
      {"namespace":"app","name":"cache","phase":"Ready","health":"ok","reasons":[],"replicas":2,"readyPods":2,"master":"cache-0",
       "pods":[{"name":"cache-0","node":"node-1","role":"master","ready":true},{"name":"cache-1","node":"node-2","role":"replica","ready":true}]}]}}
    """#

    private var services: DataServices { get throws { try TalosJSON.decode(DataServices.self, from: json) } }

    func testDecodesInstancesAndRoles() throws {
        let s = try services
        let df = try XCTUnwrap(s.dragonfly)
        XCTAssertEqual(df.instances.count, 4)
        XCTAssertEqual(df.instances[0].reasons, [.noReady])
        XCTAssertEqual(df.instances[3].pods.map(\.role), ["master", "replica"])
        XCTAssertEqual(s.detected, [.longhorn, .dragonfly])
    }

    func testSummaryAndLikelyCause() throws {
        let s = try services
        XCTAssertEqual(s.summary(.dragonfly), ServiceSummary(total: 4, attention: 3, health: .critical))
        XCTAssertEqual(s.likelyCauses(), [LikelyCause(node: "node-3", problems: 1)])
    }

    func testAlertsSkipARollingUpdate() throws {
        XCTAssertEqual(dataIssuesOf(try services), ["dragonfly|app/down": dataCritical, "dragonfly|app/queue": dataWarning])
    }

    func testHintsIncludeDragonfly() throws {
        let inventory = try TalosJSON.decode(ClusterInventory.self, from: #"{"apps":[{"id":"dragonfly","name":"Dragonfly"},{"id":"longhorn","name":"Longhorn"}]}"#)
        XCTAssertEqual(dataServiceHints(inventory), "longhorn,dragonfly")
    }
}

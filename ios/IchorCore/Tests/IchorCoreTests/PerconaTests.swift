import XCTest
@testable import IchorCore

final class PerconaTests: XCTestCase {
    private let json = #"""
    {"longhorn":{"nodes":[{"name":"node-3","ready":false}]},
     "percona":{"version":"v1","error":"","clusters":[
      {"namespace":"db","name":"down","state":"error","message":"PXC: back-off","health":"critical","reasons":["error","noMember"],
       "pxcSize":3,"pxcReady":0,"proxy":"haproxy","proxySize":2,"proxyReady":0,
       "pods":[{"name":"down-pxc-0","node":"node-3","phase":"Running","ready":false},{"name":"down-pxc-1","node":"","phase":"Pending","ready":false}]},
      {"namespace":"db","name":"crm","state":"initializing","health":"warning","reasons":["members"],"pxcSize":3,"pxcReady":2,
       "proxy":"proxysql","proxySize":2,"proxyReady":2,"pods":[{"name":"crm-pxc-0","node":"node-1","phase":"Running","ready":true}]},
      {"namespace":"db","name":"fresh","state":"initializing","health":"warning","reasons":["initializing"],"pxcSize":1,"pxcReady":1},
      {"namespace":"db","name":"wiki","state":"ready","health":"warning","reasons":["backupStale"],"pxcSize":1,"pxcReady":1,
       "lastBackupAt":1759000000000,"backupSchedules":[{"name":"daily","schedule":"0 2 * * *","keep":7,"storageName":"s3"}]},
      {"namespace":"db","name":"shop","state":"ready","health":"ok","reasons":[],"pxcSize":3,"pxcReady":3,"proxy":"haproxy","proxySize":2,"proxyReady":2},
      {"namespace":"db","name":"old","state":"paused","paused":true,"health":"idle","reasons":[],"pxcSize":3}]}}
    """#

    private var services: DataServices { get throws { try TalosJSON.decode(DataServices.self, from: json) } }

    func testDecodesClustersAndBackups() throws {
        let s = try services
        let pxc = try XCTUnwrap(s.percona)
        XCTAssertEqual(pxc.clusters.count, 6)
        XCTAssertEqual(pxc.clusters[0].reasons, [.error, .noMember])
        XCTAssertEqual(pxc.clusters[1].proxy, "proxysql")
        XCTAssertEqual(pxc.clusters[3].backupSchedules.map(\.name), ["daily"])
        XCTAssertEqual(pxc.clusters[3].lastBackupAt, 1_759_000_000_000)
        XCTAssertTrue(pxc.clusters[5].paused)
        XCTAssertEqual(s.detected, [.longhorn, .percona])
    }

    func testSummaryAndLikelyCause() throws {
        let s = try services
        XCTAssertEqual(s.summary(.percona), ServiceSummary(total: 6, attention: 4, health: .critical))
        XCTAssertEqual(s.likelyCauses(), [LikelyCause(node: "node-3", problems: 1)])
    }

    func testAlertsSkipInitializing() throws {
        XCTAssertEqual(dataIssuesOf(try services), [
            "percona|db/down": dataCritical, "percona|db/crm": dataWarning, "percona|db/wiki": dataWarning,
        ])
    }

    func testCriticalAlertNamesTheSystem() throws {
        let now = Date(timeIntervalSince1970: 1_800_000_000)
        let snap = { (issues: [String: String]) in
            ClusterSnapshot(context: "lab", takenAt: now, nodes: ["a": NodeState(hostname: "host-a", health: .ready)],
                            etcdChecked: true, certNotAfter: Int64(now.timeIntervalSince1970) + 365 * 86_400,
                            dataWatched: true, dataChecked: true, dataIssues: issues)
        }
        let result = evaluate(previous: snap([:]), current: snap(["percona|db/down": dataCritical]), now: now)
        XCTAssertEqual(try XCTUnwrap(result.alerts.first).text, "Percona XtraDB Cluster · critical")
    }

    func testHintsIncludePercona() throws {
        let inventory = try TalosJSON.decode(ClusterInventory.self, from: #"{"apps":[{"id":"percona-xtradb","name":"Percona XtraDB Cluster"},{"id":"longhorn","name":"Longhorn"}]}"#)
        XCTAssertEqual(dataServiceHints(inventory), "longhorn,percona-xtradb")
    }
}

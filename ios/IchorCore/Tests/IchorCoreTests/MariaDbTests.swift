import XCTest
@testable import IchorCore

final class MariaDbTests: XCTestCase {
    private let json = #"""
    {"longhorn":{"nodes":[{"name":"node-3","ready":false}]},
     "mariadb":{"version":"v1alpha1","error":"","clusters":[
      {"namespace":"app","name":"down","topology":"replication","health":"critical","reasons":["noReady"],"replicas":2,"readyPods":0,"primary":"down-0",
       "pods":[{"name":"down-0","node":"node-3","phase":"Running","role":"primary","ready":false},{"name":"down-1","node":"","phase":"Pending","role":"replica","ready":false}]},
      {"namespace":"app","name":"forum","topology":"galera","health":"warning","reasons":["pods","galeraRecovery"],"message":"Recovering Galera cluster","replicas":3,"readyPods":2,"primary":"forum-0",
       "pods":[{"name":"forum-0","node":"node-1","role":"primary","ready":true},{"name":"forum-1","node":"node-2","role":"member","ready":true},{"name":"forum-2","node":"node-2","role":"member","ready":false}]},
      {"namespace":"app","name":"nightly","topology":"standalone","health":"warning","reasons":["backupFailed"],"replicas":1,"readyPods":1,"primary":"nightly-0",
       "pods":[{"name":"nightly-0","node":"node-1","role":"primary","ready":true}],"lastBackupAt":1759370000000,"lastBackupFailedAt":1759460000000,"backupSchedule":"0 1 * * *"},
      {"namespace":"app","name":"rollout","topology":"replication","health":"warning","reasons":["pods"],"replicas":2,"readyPods":1,"primary":"rollout-0",
       "pods":[{"name":"rollout-0","node":"node-1","role":"primary","ready":true},{"name":"rollout-1","node":"","phase":"Pending","role":"replica","ready":false}]},
      {"namespace":"app","name":"shop","topology":"replication","health":"ok","reasons":[],"replicas":2,"readyPods":2,"primary":"shop-0",
       "pods":[{"name":"shop-0","node":"node-1","role":"primary","ready":true},{"name":"shop-1","node":"node-2","role":"replica","ready":true}]},
      {"namespace":"app","name":"paused","topology":"standalone","health":"idle","reasons":[],"suspended":true,"replicas":1,"readyPods":1}]}}
    """#

    private var services: DataServices { get throws { try TalosJSON.decode(DataServices.self, from: json) } }

    func testDecodesClustersRolesAndBackups() throws {
        let s = try services
        let db = try XCTUnwrap(s.mariadb)
        XCTAssertEqual(db.clusters.count, 6)
        XCTAssertEqual(db.clusters[1].reasons, [.pods, .galeraRecovery])
        XCTAssertEqual(db.clusters[1].message, "Recovering Galera cluster")
        XCTAssertEqual(db.clusters[1].pods.map(\.role), ["primary", "member", "member"])
        XCTAssertEqual(db.clusters[2].backupSchedule, "0 1 * * *")
        XCTAssertEqual(db.clusters[2].lastBackupFailedAt, 1_759_460_000_000)
        XCTAssertTrue(db.clusters[5].suspended)
        XCTAssertEqual(s.detected, [.longhorn, .mariadb])
    }

    func testSummaryAndLikelyCause() throws {
        let s = try services
        XCTAssertEqual(s.summary(.mariadb), ServiceSummary(total: 6, attention: 4, health: .critical))
        XCTAssertEqual(s.likelyCauses(), [LikelyCause(node: "node-3", problems: 1)])
    }

    func testAlertsSkipAReplicaRollingOut() throws {
        XCTAssertEqual(dataIssuesOf(try services), [
            "mariadb|app/down": dataCritical, "mariadb|app/forum": dataWarning, "mariadb|app/nightly": dataWarning,
        ])
    }

    func testAlertNamesMariaDB() throws {
        let now = Date(timeIntervalSince1970: 1_800_000_000)
        func snap(_ issues: [String: String]) -> ClusterSnapshot {
            ClusterSnapshot(context: "lab", takenAt: now, nodes: ["a": NodeState(hostname: "host-a", health: .ready)],
                            etcdChecked: true, certNotAfter: Int64(now.timeIntervalSince1970) + 365 * 86_400,
                            dataWatched: true, dataChecked: true, dataIssues: issues)
        }
        let alert = try XCTUnwrap(evaluate(previous: snap([:]), current: snap(["mariadb|app/down": dataCritical]), now: now).alerts.first)
        XCTAssertEqual(alert.text, "MariaDB · critical")
    }

    func testHintsIncludeMariaDB() throws {
        let inventory = try TalosJSON.decode(ClusterInventory.self, from: #"{"apps":[{"id":"mariadb","name":"MariaDB"},{"id":"longhorn","name":"Longhorn"}]}"#)
        XCTAssertEqual(dataServiceHints(inventory), "longhorn,mariadb")
    }
}

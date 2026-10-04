import XCTest
@testable import IchorCore

final class CephTests: XCTestCase {
    private let json = #"""
    {"longhorn":{"nodes":[{"name":"node-3","ready":false}]},
     "ceph":{"version":"v1","error":"","clusters":[
      {"namespace":"ceph-down","name":"down","phase":"Ready","cephHealth":"HEALTH_ERR","health":"critical","reasons":["healthErr","noOSD"],
       "checks":[{"name":"OSD_DOWN","severity":"HEALTH_WARN","message":"2 osds down"}],"bytesTotal":1000,"bytesUsed":100,
       "osdsUp":0,"osdsTotal":2,"monsReady":1,"monsTotal":1,"notReadyNodes":["node-3"],"version":"19.2.3-0","external":false},
      {"namespace":"storage","name":"ceph","phase":"Ready","cephHealth":"HEALTH_WARN","health":"warning","reasons":["healthWarn","nearFull"],
       "checks":[{"name":"OSD_NEARFULL","severity":"HEALTH_WARN","message":"1 nearfull osd(s)"}],"bytesTotal":1000,"bytesUsed":900,
       "osdsUp":3,"osdsTotal":3,"monsReady":3,"monsTotal":3,"notReadyNodes":[],"version":"19.2.3-0"},
      {"namespace":"ceph-new","name":"new","phase":"Progressing","health":"warning","reasons":["notReady"]},
      {"namespace":"ceph-ext","name":"remote","phase":"Connected","cephHealth":"HEALTH_OK","health":"ok","reasons":[],"external":true}],
     "pools":[
      {"namespace":"storage","name":"broken","kind":"blockPool","phase":"Failure","health":"critical"},
      {"namespace":"storage","name":"fs","kind":"filesystem","phase":"Progressing","health":"warning"},
      {"namespace":"storage","name":"s3","kind":"objectStore","phase":"Ready","health":"ok"}],
     "osds":[
      {"namespace":"ceph-down","id":"0","pod":"rook-ceph-osd-0-abc","node":"node-3","phase":"Running","ready":false},
      {"namespace":"storage","id":"1","pod":"rook-ceph-osd-1-abc","node":"node-1","phase":"Running","ready":true}]}}
    """#

    private var services: DataServices { get throws { try TalosJSON.decode(DataServices.self, from: json) } }

    func testDecodesClustersPoolsAndOSDs() throws {
        let s = try services
        let ceph = try XCTUnwrap(s.ceph)
        XCTAssertEqual(ceph.clusters.count, 4)
        XCTAssertEqual(ceph.clusters[0].reasons, [.healthErr, .noOSD])
        XCTAssertEqual(ceph.clusters[1].checks.map(\.name), ["OSD_NEARFULL"])
        XCTAssertEqual(ceph.clusters[1].usedFraction, 0.9, accuracy: 1e-9)
        XCTAssertEqual(ceph.clusters[2].usedFraction, 0)
        XCTAssertTrue(ceph.clusters[3].external)
        XCTAssertEqual(ceph.pools.map(\.kind), ["blockPool", "filesystem", "objectStore"])
        XCTAssertEqual(ceph.osds.map(\.osdID), ["0", "1"])
        XCTAssertEqual(s.detected, [.longhorn, .ceph])
    }

    func testSummaryAndLikelyCause() throws {
        let s = try services
        // Three clusters and two pools need a look; the total counts clusters.
        XCTAssertEqual(s.summary(.ceph), ServiceSummary(total: 4, attention: 5, health: .critical))
        XCTAssertEqual(s.likelyCauses(), [LikelyCause(node: "node-3", problems: 1)])
    }

    func testAlertsSkipAClusterBeingSetUp() throws {
        XCTAssertEqual(dataIssuesOf(try services),
                       ["ceph|ceph-down/down": dataCritical, "ceph|blockPool/storage/broken": dataCritical, "ceph|storage/ceph": dataWarning])
    }

    func testHintsIncludeRook() throws {
        let inventory = try TalosJSON.decode(ClusterInventory.self, from: #"{"apps":[{"id":"rook","name":"Rook Ceph"},{"id":"longhorn","name":"Longhorn"}]}"#)
        XCTAssertEqual(dataServiceHints(inventory), "longhorn,rook")
    }
}

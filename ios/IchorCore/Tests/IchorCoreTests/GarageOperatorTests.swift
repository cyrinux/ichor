import XCTest
@testable import IchorCore

final class GarageOperatorTests: XCTestCase {
    private let json = #"""
    {"garage":{"error":"",
     "instances":[{"namespace":"garage","name":"garage","pod":"garage-0","pods":3,"podsReady":3,"version":"v2.3.0",
       "status":"degraded","message":"1 node down","connectedNodes":2,"knownNodes":3,"storageNodes":3,"storageNodesUp":2,
       "partitions":256,"partitionsQuorum":256,"partitionsAllOk":171,"resyncQueue":10,"resyncErrors":4,"tableSyncQueue":0,
       "layoutVersion":12,"nodes":[],"source":"cli-json"}],
     "version":"v1beta2","truncated":false,
     "clusters":[
      {"namespace":"garage","name":"garage","instance":"garage/garage","phase":"Degraded","clusterId":"1b7e","garageVersion":"v2.3.0",
       "replicas":5,"readyReplicas":4,"storageReplicas":3,"storageReadyReplicas":2,"gatewayReplicas":2,"gatewayReadyReplicas":2,
       "layoutVersion":12,"stagedLayoutVersion":13,"pendingGatewayTombstones":["gn-gw-0"],
       "conditions":[{"type":"QuorumAtRisk","status":"True","reason":"PartitionsBelowQuorum","message":"1/3 partitions without write quorum","time":100}],
       "health":"warning","reasons":["quorumAtRisk","storageReplicas","layoutStaged","tombstones"]},
      {"namespace":"edge","name":"edge","instance":"","phase":"Running","health":"critical","reasons":["adminUnreachable"]}],
     "buckets":[
      {"namespace":"garage","name":"backups","instance":"garage/garage","cluster":"garage","phase":"Ready","bucketId":"a3f8",
       "size":"3.0 GiB","maxSizeQuota":"10Gi","quotaUsage":{"sizeBytes":3070230528,"sizeLimit":10737418240,"sizePercent":28,"objectCount":1204},
       "websiteEnabled":false,"websiteUrl":"","keysWithAccess":2,
       "conditions":[{"type":"LifecycleConfigured","status":"False","reason":"GarageTooOld","message":"lifecycle rules need Garage v2.3.0 or newer","time":200}],
       "health":"warning","reasons":["lifecycleNotApplied"]},
      {"namespace":"garage","name":"files","instance":"garage/garage","cluster":"garage","phase":"Ready","bucketId":"4f1d",
       "size":"52 MiB","quotaUsage":{"sizeBytes":54525952,"objectCount":312},
       "websiteEnabled":true,"websiteUrl":"https://files.example.com","keysWithAccess":1,"health":"ok","reasons":[]}],
     "keys":[
      {"namespace":"garage","name":"backup-agent","instance":"garage/garage","cluster":"garage","phase":"Ready","keyId":"e41a",
       "clusterWide":true,"allBuckets":{"read":true,"write":true,"owner":true},"bucketPermissions":[],"bucketPermissionsTotal":0,
       "health":"ok","reasons":[]},
      {"namespace":"garage","name":"app-writer","instance":"garage/garage","cluster":"garage","phase":"Ready","keyId":"6d03",
       "clusterWide":false,"bucketPermissions":[{"bucket":"files","read":true,"write":true,"owner":false}],"bucketPermissionsTotal":25,
       "health":"ok","reasons":[]}],
     "nodes":[
      {"namespace":"garage","name":"garage-2xv9p","instance":"garage/garage","cluster":"garage","phase":"Degraded",
       "nodeId":"9c2f","zone":"zone-3","capacity":"500Gi","gateway":false,"connected":false,"inLayout":true,"lastSeenSecs":172800,
       "health":"warning","reasons":["nodeDisconnected"]}],
     "operator":{"namespace":"garage-operator-system","name":"garage-operator","ready":1,"desired":1,"health":"ok","reasons":[]}}}
    """#

    private var services: DataServices { get throws { try TalosJSON.decode(DataServices.self, from: json) } }

    func testDecodesTheOperatorLayer() throws {
        let g = try XCTUnwrap(services.garage)
        XCTAssertTrue(g.hasOperator)
        XCTAssertEqual(g.version, "v1beta2")
        XCTAssertFalse(g.truncated)

        let degraded = try XCTUnwrap(g.clusters.first)
        XCTAssertEqual(degraded.instance, "garage/garage")
        XCTAssertEqual(degraded.reasons.first, .quorumAtRisk)
        XCTAssertEqual(degraded.stagedLayoutVersion, 13)
        XCTAssertEqual(degraded.pendingGatewayTombstones, ["gn-gw-0"])
        XCTAssertEqual(degraded.health, .warning)
        XCTAssertEqual(degraded.conditions.first?.message, "1/3 partitions without write quorum")
        // The unmatched handle stays at the section root.
        XCTAssertEqual(g.clusters.last?.instance, "")
        XCTAssertEqual(g.clusters.last?.health, .critical)

        let backups = try XCTUnwrap(g.buckets.first)
        XCTAssertEqual(backups.quotaUsage?.sizePercent, 28)
        XCTAssertEqual(backups.maxSizeQuota, "10Gi")
        XCTAssertEqual(backups.reasons, [.lifecycleNotApplied])
        XCTAssertEqual(g.buckets.last?.websiteURL, "https://files.example.com")

        let agent = try XCTUnwrap(g.keys.first)
        XCTAssertTrue(agent.clusterWide)
        XCTAssertTrue(try XCTUnwrap(agent.allBuckets).owner)
        XCTAssertNil(g.keys.last?.allBuckets)
        XCTAssertEqual(g.keys.last?.bucketPermissionsTotal, 25)
        XCTAssertEqual(g.keys.last?.bucketPermissions.first?.bucket, "files")

        let down = try XCTUnwrap(g.nodes.first)
        XCTAssertFalse(down.connected)
        XCTAssertTrue(down.inLayout)
        XCTAssertEqual(down.lastSeenSecs, 172800)
        XCTAssertEqual(down.reasons, [.nodeDisconnected])

        let op = try XCTUnwrap(g.`operator`)
        XCTAssertEqual(op.ready, 1)
        XCTAssertEqual(op.label, "garage-operator-system/garage-operator")
    }

    func testDecodesWithoutTheOperator() throws {
        let plain = try TalosJSON.decode(
            GarageStatus.self,
            from: #"{"error":"","instances":[{"namespace":"garage","name":"garage","status":"healthy"}]}"#)
        XCTAssertFalse(plain.hasOperator)
        XCTAssertTrue(plain.clusters.isEmpty)
        XCTAssertNil(plain.`operator`)
    }

    func testAlertsCoverClustersBucketsAndNodeDeclarations() throws {
        let issues = dataIssuesOf(try services)
        // The degraded instance, the degraded cluster, the failed handle, the bucket whose
        // lifecycle was dropped and the disconnected node declaration.
        XCTAssertEqual(issues["garage|garage/garage"], dataWarning)
        XCTAssertEqual(issues["garage|edge/edge"], dataCritical)
        XCTAssertEqual(issues["garage|bucket/garage/backups"], dataWarning)
        XCTAssertEqual(issues["garage|node/garage/garage-2xv9p"], dataWarning)
        XCTAssertNil(issues["garage|garage/garage-2xv9p"])
    }
}

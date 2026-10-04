import XCTest
@testable import IchorCore

final class DataServicesTests: XCTestCase {
    // Shaped like KubeDataServices' answer on a cluster with one node down.
    private let json = #"""
    {"longhorn":{"version":"v1beta2","error":"","volumes":[
      {"name":"pvc-1","namespace":"longhorn-system","pvcNamespace":"app","pvcName":"search","state":"detached","robustness":"faulted","health":"critical",
       "replicasDesired":1,"replicasHealthy":0,"rebuilding":0,"replicaNodes":["node-3"],"node":"","size":10,"actualSize":5,"lastBackupAt":0},
      {"name":"pvc-2","namespace":"longhorn-system","pvcNamespace":"app","pvcName":"db","state":"attached","robustness":"healthy","health":"ok","replicasDesired":3,"replicasHealthy":3,"replicaNodes":["node-1","node-2","node-3"],
       "rebuilding":1,"rebuildProgress":42,"backingUp":true,"backupProgress":63,"restoring":false,"restoreProgress":0,"scheduleError":"insufficient storage","tooManySnapshots":true},
      {"name":"pvc-3","state":"detached","robustness":"unknown","health":"idle","replicaNodes":["node-1"]}],
     "nodes":[{"name":"node-1","namespace":"longhorn-system","ready":true,"schedulable":false,"allowScheduling":false,"evictionRequested":true,"replicas":2,"disks":[{"path":"/var/lib/longhorn","schedulable":true,"available":1,"maximum":2,"scheduled":1}]},
              {"name":"node-3","ready":false,"schedulable":false,"disks":[]}],
     "backupTargets":[{"name":"default","url":"s3://b@garage/","available":true,"message":""}]},
     "garage":{"error":"","instances":[
      {"namespace":"storage","name":"garage","pod":"garage-a","pods":3,"podsReady":2,"version":"v2.3.0","status":"degraded",
       "message":"1 node down (zone z3, tags node-3)","storageNodes":3,"storageNodesUp":2,"partitions":256,"partitionsQuorum":256,"partitionsAllOk":170,
       "resyncQueue":12,"resyncErrors":10,"tableSyncQueue":4,"layoutVersion":3,"source":"cli-json","futureField":true,
       "nodes":[{"id":"aa","hostname":"","zone":"z3","tags":["node-3"],"kubeNode":"","storage":true,"up":false,"lastSeenSecs":-1,"resyncQueue":-1,"resyncErrors":-1,"tableSyncQueue":-1,"statsError":"Not connected"},
                {"id":"bb","hostname":"garage-a","zone":"z1","tags":["node-1"],"kubeNode":"node-1","storage":true,"up":true,"lastSeenSecs":-1,"dataAvail":1,"dataTotal":2,"resyncQueue":6,"resyncErrors":5,"tableSyncQueue":2,"tranquility":0}]},
      {"namespace":"nas","name":"garage-nas","pods":1,"podsReady":1,"status":"healthy","source":"cli-json"}]},
     "cnpg":{"version":"v1","error":"","clusters":[
      {"namespace":"app","name":"down-db","phase":"Waiting for the instances to become active","health":"critical","hibernated":false,"reasons":["noInstance","someNewReason"],
       "instances":2,"readyInstances":0,"currentPrimary":"down-db-1","targetPrimary":"down-db-1",
       "instancePods":[{"name":"down-db-1","node":"node-3","phase":"Running","role":"primary","ready":false},{"name":"down-db-2","node":"","phase":"Pending","role":"","ready":false}],
       "archiving":"ok","lastBackup":"failed","backupMethod":"plugin","objectStore":"garage-store","scheduled":true,
       "lastSuccessfulBackupAt":1000,"lastFailedBackupAt":2000,"firstRecoverabilityAt":500},
      {"namespace":"app","name":"ok-db","phase":"Cluster in healthy state","health":"ok","reasons":[],"instances":2,"readyInstances":2,"archiving":"off","lastBackup":"ok"}]}}
    """#

    private var services: DataServices { get throws { try TalosJSON.decode(DataServices.self, from: json) } }

    func testDecodesTheGoAnswer() throws {
        let s = try services
        let lh = try XCTUnwrap(s.longhorn)
        XCTAssertEqual(lh.volumes.count, 3)
        XCTAssertEqual(lh.volumes[0].health, .critical)
        XCTAssertEqual(lh.volumes[0].label, "app/search")
        XCTAssertEqual(lh.volumes[2].label, "pvc-3")

        let garage = try XCTUnwrap(s.garage?.instances.first)
        XCTAssertEqual(garage.state, .degraded)
        XCTAssertTrue(garage.detailed)
        XCTAssertEqual(garage.nodes[0].label, "node-3")
        XCTAssertEqual(garage.nodes[1].label, "garage-a")
        XCTAssertEqual(garage.nodes[0].tranquility, -1)
        XCTAssertEqual(garage.nodes[1].tranquility, GarageTranquility.fullSpeed)

        let down = try XCTUnwrap(s.cnpg?.clusters.first)
        XCTAssertEqual(down.reasons, [.noInstance])
        XCTAssertEqual(down.lastSuccessAt, 1000)
        XCTAssertEqual(down.recoverableAt, 500)
        XCTAssertEqual(s.detected, [.longhorn, .garage, .cnpg])
    }

    func testEmptyAnswerHasNoSection() throws {
        let none = try TalosJSON.decode(DataServices.self, from: "{}")
        XCTAssertTrue(none.detected.isEmpty)
        XCTAssertNil(none.summary(.longhorn))
        XCTAssertEqual(none.worst, .ok)
    }

    func testUnknownWireValues() {
        XCTAssertEqual(ServiceHealth(wire: "bogus"), .unknown)
        XCTAssertEqual(ServiceHealth(wire: ""), .unknown)
        XCTAssertNil(GarageState(rawValue: "weird"))
        XCTAssertNil(CnpgReason(rawValue: "someNewReason"))
    }

    func testSummaries() throws {
        let s = try services
        // The faulted volume and the node that is not ready.
        XCTAssertEqual(s.summary(.longhorn), ServiceSummary(total: 3, attention: 2, health: .critical))
        XCTAssertEqual(s.summary(.garage), ServiceSummary(total: 2, attention: 1, health: .warning))
        XCTAssertEqual(s.summary(.cnpg), ServiceSummary(total: 2, attention: 1, health: .critical))
        XCTAssertEqual(s.worst, .critical)
        XCTAssertEqual(s.cnpg?.pendingInstances, 1)
    }

    func testSummaryOfAnUnreadableSystem() throws {
        let broken = try TalosJSON.decode(DataServices.self, from: #"{"longhorn":{"error":"Kubernetes API: permission denied"}}"#)
        XCTAssertEqual(broken.summary(.longhorn)?.health, .unknown)
        XCTAssertEqual(broken.summary(.longhorn)?.error, "Kubernetes API: permission denied")
    }

    func testIdleOnlyIsOk() throws {
        let idle = try TalosJSON.decode(DataServices.self, from: #"{"longhorn":{"volumes":[{"name":"v","state":"detached","health":"idle"}]}}"#)
        XCTAssertEqual(idle.summary(.longhorn), ServiceSummary(total: 1, attention: 0, health: .ok))
    }

    func testHintsFromTheInventory() throws {
        let inventory = try TalosJSON.decode(ClusterInventory.self, from: #"{"apps":[{"id":"cloudnative-pg","name":"CloudNativePG"},{"id":"nginx","name":"nginx"},{"id":"longhorn","name":"Longhorn"}]}"#)
        XCTAssertEqual(dataServiceHints(inventory), "longhorn,cloudnative-pg")
        let other = try TalosJSON.decode(ClusterInventory.self, from: #"{"apps":[{"id":"nginx","name":"nginx"}]}"#)
        XCTAssertEqual(dataServiceHints(other), "")
    }

    func testLikelyCausePointsToTheNodeThatIsDown() throws {
        let s = try services
        // node-3 is not ready for Longhorn: the faulted volume, the Garage cluster missing a node
        // tagged node-3 and the Postgres cluster with an instance there all point to it.
        XCTAssertEqual(s.likelyCauses(), [LikelyCause(node: "node-3", problems: 3)])
        // A node reported down elsewhere (Talos) that explains nothing is not listed.
        XCTAssertEqual(s.likelyCauses(downNodes: ["node-9"]), [LikelyCause(node: "node-3", problems: 3)])
    }

    func testDecodesTheLonghornProgressAndNodeSettings() throws {
        let lh = try XCTUnwrap(try services.longhorn)
        let busy = lh.volumes[1]
        XCTAssertEqual(busy.rebuildProgress, 42)
        XCTAssertTrue(busy.backingUp)
        XCTAssertEqual(busy.backupProgress, 63)
        XCTAssertFalse(busy.restoring)
        XCTAssertEqual(busy.scheduleError, "insufficient storage")
        XCTAssertTrue(busy.tooManySnapshots)
        // Fields an older core does not send fall back to their defaults.
        let old = lh.volumes[2]
        XCTAssertEqual(old.rebuildProgress, 0)
        XCTAssertFalse(old.backingUp)
        XCTAssertEqual(old.scheduleError, "")
        XCTAssertFalse(old.tooManySnapshots)

        let evicting = lh.nodes[0]
        XCTAssertEqual(evicting.namespace, "longhorn-system")
        XCTAssertFalse(evicting.allowScheduling)
        XCTAssertTrue(evicting.evictionRequested)
        XCTAssertEqual(evicting.replicas, 2)
        let down = lh.nodes[1]
        XCTAssertEqual(down.namespace, "")
        XCTAssertTrue(down.allowScheduling)
        XCTAssertFalse(down.evictionRequested)
        XCTAssertEqual(down.replicas, 0)
    }

    func testLonghornActions() throws {
        let lh = try XCTUnwrap(try services.longhorn)
        // Detached: only the replica count; attached: backup and trim too.
        XCTAssertEqual(lh.volumes[0].actions, [.replicas])
        XCTAssertEqual(lh.volumes[1].actions, [.backup, .trim, .replicas])
        // No Longhorn namespace (an older core): nothing to act on.
        XCTAssertEqual(lh.volumes[2].actions, [])
        XCTAssertEqual(lh.nodes[0].actions, [.schedulingOn, .cancelEviction])
        XCTAssertEqual(lh.nodes[1].actions, [])

        let node = try TalosJSON.decode(LonghornNode.self, from: #"{"name":"n","namespace":"longhorn-system","allowScheduling":true}"#)
        XCTAssertEqual(node.actions, [.schedulingOff, .evict])
        XCTAssertEqual(LonghornAction.cancelEviction.rawValue, "cancelEviction")
        XCTAssertEqual(LonghornAction.schedulingOff.rawValue, "schedulingOff")
    }

    func testReplicaBounds() throws {
        let three = try XCTUnwrap(try services.longhorn?.volumes[1])
        XCTAssertEqual(three.maxReplicas(nodes: 5), 5)
        // Never below the current count, even with fewer nodes.
        XCTAssertEqual(three.maxReplicas(nodes: 2), 3)
        XCTAssertEqual(three.maxReplicas(nodes: 40), LonghornReplicas.max)
        XCTAssertEqual(three.defaultReplicas(nodes: 2), 3)
        let unset = try TalosJSON.decode(LonghornVolume.self, from: #"{"name":"v"}"#)
        XCTAssertEqual(unset.maxReplicas(nodes: 0), 1)
        XCTAssertEqual(unset.defaultReplicas(nodes: 3), 1)
    }

    func testFilters() throws {
        let s = try services
        let volumes = try XCTUnwrap(s.longhorn?.volumes)
        XCTAssertEqual(filterVolumes(volumes, filter: .problems, query: "").map(\.name), ["pvc-1"])
        XCTAssertEqual(filterVolumes(volumes, filter: .detached, query: "").map(\.name), ["pvc-1", "pvc-3"])
        XCTAssertEqual(filterVolumes(volumes, filter: .all, query: "APP/DB").map(\.name), ["pvc-2"])

        let clusters = try XCTUnwrap(s.cnpg?.clusters)
        XCTAssertEqual(filterClusters(clusters, problemsOnly: true, query: "").map(\.name), ["down-db"])
        XCTAssertEqual(filterClusters(clusters, problemsOnly: false, query: "ok").map(\.name), ["ok-db"])
    }
}

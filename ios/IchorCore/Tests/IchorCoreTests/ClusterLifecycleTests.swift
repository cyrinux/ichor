import XCTest
@testable import IchorCore

final class ClusterLifecycleTests: XCTestCase {
    // Go KubeClusterAPI's demo answer, trimmed to a few rows of each kind.
    private let json = #"""
        {"installed":true,"version":"v1beta2","sidero":true,"talosControlPlane":true,"talosBootstrap":true,"truncated":false,
         "clusters":[
          {"namespace":"fleet","name":"old-lab","phase":"Failed","endpoint":"10.0.2.5:6443","controlPlane":{"desired":1,"ready":0},
           "workers":{"desired":0,"ready":0},"versions":["v1.33.4"],"conditions":[],"failureReason":"CreateError",
           "failureMessage":"the control plane machine failed to provision","health":"critical","reasons":["clusterFailed"]},
          {"namespace":"fleet","name":"staging","phase":"Provisioning","controlPlane":{"desired":1,"ready":0},"workers":{"desired":4,"ready":2},
           "conditions":[{"type":"ControlPlaneInitialized","status":"False","reason":"NotInitialized","message":"waiting for the first control plane machine"}],
           "health":"warning","reasons":["clusterProvisioning"]},
          {"namespace":"fleet","name":"prod-edge","phase":"Provisioned","controlPlane":null,"workers":{"desired":4,"ready":4},"health":"ok","reasons":[]}],
         "machines":[
          {"namespace":"fleet","name":"old-lab-cp-q9d4m","cluster":"old-lab","controlPlane":true,"phase":"Failed","failureReason":"CreateError",
           "failureMessage":"server srv-b2 did not boot from disk","health":"critical"},
          {"namespace":"fleet","name":"staging-cp-x7k2p","cluster":"staging","controlPlane":true,"phase":"Provisioning","stuck":true,
           "lastUpdated":1791900000000,"health":"warning"},
          {"namespace":"fleet","name":"prod-edge-workers-6b8d-k2m9x","cluster":"prod-edge","controlPlane":false,"phase":"Running","node":"prod-edge-w-1","health":"ok"},
          {"namespace":"fleet","name":"prod-edge-cp-4fj8s","cluster":"prod-edge","controlPlane":true,"phase":"Running","node":"prod-edge-cp-1","health":"ok"}],
         "machineDeployments":[{"namespace":"fleet","name":"staging-workers","cluster":"staging","phase":"ScalingUp","desired":4,"ready":2,"available":2,
           "version":"v1.34.1","health":"warning"}],
         "servers":[
          {"name":"srv-b2","hostname":"rack2-u02","accepted":true,"inUse":true,"power":"off","serverClasses":["any"],"cpu":"AMD EPYC 7313P, 16 cores",
           "memoryBytes":137438953472,"health":"critical"},
          {"name":"srv-b1","hostname":"","accepted":false,"power":"unknown","health":"warning"}],
         "serverClasses":[{"name":"any","available":1,"inUse":3},{"name":"large","available":0,"inUse":1}],
         "counts":{"clusters":3,"machines":9,"servers":5,"ok":12,"warning":4,"critical":3}}
        """#

    private func decode(_ text: String) throws -> ClusterLifecycle {
        try JSONDecoder().decode(ClusterLifecycle.self, from: Data(text.utf8))
    }

    func testDecodesEveryKind() throws {
        let r = try decode(json)
        XCTAssertTrue(r.installed && r.sidero && r.talosControlPlane)
        XCTAssertEqual(r.clusters.map(\.name), ["old-lab", "staging", "prod-edge"])
        XCTAssertEqual(r.clusters[0].health, .critical)
        XCTAssertEqual(r.clusters[0].controlPlane?.ratio, "0/1")
        XCTAssertNil(r.clusters[2].controlPlane)
        XCTAssertTrue(r.machines[1].stuck)
        XCTAssertEqual(r.machineDeployments.first?.name, "staging-workers")
        XCTAssertEqual(r.servers[0].power, "off")
        XCTAssertEqual(r.servers[1].title, "srv-b1")
        XCTAssertFalse(r.servers[1].accepted)
        XCTAssertFalse(r.serverClasses[0].exhausted)
        XCTAssertTrue(r.serverClasses[1].exhausted)
        XCTAssertEqual(r.counts.critical, 3)
        XCTAssertFalse(r.isEmpty)
    }

    func testProblemIsTheFailureElseTheConditions() throws {
        let r = try decode(json)
        XCTAssertEqual(r.clusters[0].problem, "CreateError: the control plane machine failed to provision")
        XCTAssertEqual(r.clusters[1].problem, "ControlPlaneInitialized: waiting for the first control plane machine")
        XCTAssertEqual(r.clusters[2].problem, "")
    }

    func testMachinesGroupByClusterControlPlaneFirst() throws {
        let groups = try decode(json).machinesByCluster
        XCTAssertEqual(groups.map(\.cluster), ["old-lab", "staging", "prod-edge"])
        XCTAssertEqual(groups[2].machines.map(\.name), ["prod-edge-cp-4fj8s", "prod-edge-workers-6b8d-k2m9x"])
    }

    func testStuckSinceFallsBackToCreation() throws {
        let r = try decode(json)
        XCTAssertEqual(r.machines[1].stuckSince, 1_791_900_000_000)
        let created = try decode(#"{"machines":[{"name":"m","cluster":"c","createdAt":5,"stuck":true}]}"#)
        XCTAssertEqual(created.machines[0].stuckSince, 5)
    }

    func testClustersOfTheSameNameInTwoNamespacesStayApart() throws {
        let r = try decode(
            #"{"machines":[{"namespace":"a","name":"m1","cluster":"c"},{"namespace":"b","name":"m2","cluster":"c"}]}"#)
        XCTAssertEqual(r.machinesByCluster.map(\.id), ["a/c", "b/c"])
        XCTAssertEqual(r.machinesByCluster.map(\.cluster), ["c", "c"])
    }

    func testVersionsAndScopes() throws {
        let r = try decode(json)
        XCTAssertEqual(r.apiVersion(.machine), "v1beta2")
        XCTAssertEqual(r.apiVersion(.server), LifecycleKind.sideroVersion)
        XCTAssertEqual(try decode("{}").apiVersion(.cluster), LifecycleKind.capiDefaultVersion)
        XCTAssertTrue(LifecycleKind.machine.namespaced)
        XCTAssertFalse(LifecycleKind.serverClass.namespaced)
        XCTAssertEqual(LifecycleKind.machineDeployment.resource, "machinedeployments")
    }

    func testNotInstalledDecodesEmpty() throws {
        let r = try decode(#"{"installed":false}"#)
        XCTAssertFalse(r.installed)
        XCTAssertTrue(r.isEmpty)
    }
}

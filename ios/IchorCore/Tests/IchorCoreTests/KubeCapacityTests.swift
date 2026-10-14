import XCTest
@testable import IchorCore

final class KubeCapacityTests: XCTestCase {
    // The shape of Go KubeCapacity's demo answer, trimmed.
    private let capacityJSON = #"""
    {"metricsAvailable":true,"priorityClassesRead":true,"pdbsRead":true,
     "totals":{"cpuAllocatable":16,"cpuRequests":10.5,"memoryAllocatable":34359738368,"memoryRequests":17179869184,
       "pods":40,"podCapacity":220,"nodes":2,"schedulableNodes":2},
     "nodes":[{"name":"demo-worker-1","roles":[],"ready":true,"cordoned":false,"taints":[],"kubelet":"v1.34.1",
       "memoryPercent":73.75,"memoryAllocatable":17179869184,"memoryUsage":8589934592,"cpuFree":0.6,"memoryFree":4509715661,
       "podsFree":49,"overcommitted":true},
       {"name":"demo-cp-1","roles":["control-plane"],"ready":true,"taints":["node-role.kubernetes.io/control-plane:NoSchedule"]}],
     "workloads":[
       {"kind":"Deployment","namespace":"demo","name":"batch-importer","replicas":2,"noPriorityClass":true,"qos":"BestEffort",
        "pdbAllowed":-1,"risk":"evictFirst","moreFit":98},
       {"kind":"Deployment","namespace":"demo","name":"report-cache","replicas":1,"priorityClassName":"batch-low","priority":100,
        "qos":"Burstable","oomKills":3,"risk":"oom","moreFit":2},
       {"kind":"DaemonSet","namespace":"kube-system","name":"kube-proxy","replicas":5,"priorityClassName":"system-node-critical",
        "priority":2000001000,"qos":"Burstable","risk":"unbounded","moreFitNA":true},
       {"kind":"Deployment","namespace":"demo","name":"next","qos":"SomethingNew","risk":"somethingNew"}],
     "nPlusOne":{"ok":false,"node":"demo-worker-1","pendingPods":9,"shortCPU":1.9,"shortMemory":3543348019},
     "assumptions":["Fit counts requests against allocatable only."]}
    """#

    private func capacity() throws -> KubeCapacity {
        try JSONDecoder().decode(KubeCapacity.self, from: Data(capacityJSON.utf8))
    }

    func testDecodesTheCoreJSON() throws {
        let c = try capacity()
        XCTAssertTrue(c.metricsAvailable)
        XCTAssertEqual(c.nodes.count, 2)
        XCTAssertEqual(c.totals.cpuFree, 5.5, accuracy: 1e-9)
        XCTAssertEqual(c.totals.podsFree, 180)
        XCTAssertFalse(c.nPlusOne.ok)
        XCTAssertEqual(c.nPlusOne.node, "demo-worker-1")
        XCTAssertEqual(c.nPlusOne.pendingPods, 9)
        XCTAssertEqual(c.assumptions.count, 1)
    }

    func testAnEmptyAnswerDecodes() throws {
        let c = try JSONDecoder().decode(KubeCapacity.self, from: Data("{}".utf8))
        XCTAssertTrue(c.nodes.isEmpty)
        XCTAssertTrue(c.nPlusOne.ok)
        XCTAssertFalse(c.metricsAvailable)
    }

    func testNodesKnowWhetherPodsMayLand() throws {
        let nodes = try capacity().nodes
        XCTAssertTrue(nodes[0].schedulable)
        XCTAssertTrue(nodes[0].overcommitted)
        XCTAssertEqual(nodes[0].memoryUsageFraction, 0.5, accuracy: 1e-9)
        XCTAssertFalse(nodes[1].schedulable)
        XCTAssertEqual(nodes[1].memoryUsageFraction, 0)
    }

    func testUnknownWordsFallBackAndTheOrderIsKept() throws {
        let w = try capacity().workloads
        XCTAssertEqual(w.map(\.name), ["batch-importer", "report-cache", "kube-proxy", "next"])
        XCTAssertEqual(w[0].qos, .bestEffort)
        XCTAssertEqual(w[0].risk, .evictFirst)
        XCTAssertEqual(w[1].risk, .oom)
        XCTAssertEqual(w[3].qos, .unknown)
        XCTAssertEqual(w[3].risk, .ok)
        XCTAssertFalse(w[3].atRisk)
        XCTAssertTrue(w[2].isSystem)
    }

    func testFiltersKeepTheDeliveredOrder() throws {
        let w = try capacity().workloads
        XCTAssertEqual(w.filtered(.all).count, 4)
        XCTAssertEqual(w.filtered(.atRisk).map(\.name), ["batch-importer", "report-cache", "kube-proxy"])
        XCTAssertEqual(w.filtered(.noPriorityClass).map(\.name), ["batch-importer"])
    }

    func testSystemClassesByNameAndValue() {
        XCTAssertTrue(isSystemClass("system-cluster-critical", priority: 0))
        XCTAssertTrue(isSystemClass("custom", priority: 1_500_000_000))
        XCTAssertFalse(isSystemClass("web-high", priority: 10000))
    }

    func testFreeFractionIsBounded() {
        XCTAssertEqual(freeFraction(1, of: 4), 0.25, accuracy: 1e-9)
        XCTAssertEqual(freeFraction(5, of: 4), 1)
        XCTAssertEqual(freeFraction(1, of: 0), 0)
    }

    func testDecodesANodeLossPlan() throws {
        let json = #"""
        {"node":"demo-worker-1","worstCase":true,"cpuLost":1.55,"memoryLost":1965031424,"podsLost":6,"daemonSetPods":1,
         "placed":[{"namespace":"demo","name":"postgres-0","workload":"StatefulSet/postgres","priority":10000,"to":"demo-worker-2"}],
         "preempted":[{"namespace":"demo","name":"worker-6f4b8-uvwxy","workload":"Deployment/worker","priority":0,
           "priorityClassName":"","node":"demo-worker-2","by":"demo/postgres-0","pdbViolated":false}],
         "pending":[{"namespace":"demo","name":"worker-6f4b8-uvwxy","workload":"Deployment/worker","qos":"Burstable","reason":"memory"}],
         "suggestions":[{"kind":"Deployment","namespace":"demo","name":"batch-importer","replicas":2,"scaleTo":0,"noPriorityClass":true,
           "frees":{"cpu":0.5,"memory":1073741824,"pods":2}},
           {"kind":"StatefulSet","namespace":"demo","name":"postgres","replicas":1,"scaleTo":1,"pdb":"postgres","pdbMinAvailable":1,"pdbBlocks":true},
           {"kind":"CronJob","namespace":"demo","name":"nightly","replicas":1,"scaleTo":0}],
         "covered":false,"candidates":[{"name":"demo-worker-1","pending":2,"preempted":1},{"name":"demo-worker-2","pending":1}],
         "assumptions":["An estimate."]}
        """#
        let plan = try JSONDecoder().decode(NodeLossPlan.self, from: Data(json.utf8))
        XCTAssertTrue(plan.worstCase)
        XCTAssertFalse(plan.covered)
        XCTAssertEqual(plan.preempted.first?.by, "demo/postgres-0")
        XCTAssertEqual(plan.pending.first?.reason, "memory")
        XCTAssertEqual(plan.candidates.map(\.name), ["demo-worker-1", "demo-worker-2"])
        XCTAssertEqual(plan.suggestions[0].freesMemory, 1073741824)
        XCTAssertTrue(plan.suggestions[1].pdbBlocks)
        // The worst case carries no explicit selection; an older payload without `nodes` decodes empty.
        XCTAssertEqual(plan.nodes, [])

        let picked = try JSONDecoder().decode(NodeLossPlan.self, from: Data(
            #"{"node":"demo-worker-1, demo-worker-2","nodes":["demo-worker-1","demo-worker-2"],"worstCase":false}"#.utf8))
        XCTAssertEqual(picked.nodes, ["demo-worker-1", "demo-worker-2"])
        XCTAssertFalse(picked.worstCase)

        // A suggestion opens the browser's scale sheet of its workload; a kind it cannot scale, none.
        XCTAssertEqual(plan.suggestions[0].scaleResource?.resource, "deployments")
        XCTAssertEqual(plan.suggestions[0].scaleResource?.scalable, true)
        XCTAssertEqual(plan.suggestions[1].scaleResource?.resource, "statefulsets")
        XCTAssertNil(plan.suggestions[2].scaleResource)
    }
}

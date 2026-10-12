package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class KubeCapacityTest {

    // The shape of Go KubeCapacity's demo answer, trimmed.
    private val capacityJson = """{"metricsAvailable":true,"metricsForbidden":false,"priorityClassesRead":true,"pdbsRead":true,
        "priorityClasses":[{"name":"web-high","value":10000,"globalDefault":false,"preemptionPolicy":"PreemptLowerPriority"}],
        "totals":{"cpuAllocatable":16,"cpuRequests":10.5,"memoryAllocatable":34359738368,"memoryRequests":17179869184,
          "pods":40,"podCapacity":220,"nodes":2,"schedulableNodes":2},
        "nodes":[{"name":"demo-worker-1","roles":[],"ready":true,"cordoned":false,"taints":[],"kubelet":"v1.34.1",
          "cpuRequests":7.4,"cpuAllocatable":8,"cpuPercent":92.5,"memoryRequests":12670153523,"memoryAllocatable":17179869184,
          "memoryPercent":73.75,"pods":61,"podCapacity":110,"memoryUsage":8589934592,"cpuFree":0.6,"memoryFree":4509715661,
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
        "assumptions":["Fit counts requests against allocatable only."]}"""

    @Test
    fun decodesTheCoreJson() {
        val c = TalosJson.decodeFromString(KubeCapacity.serializer(), capacityJson)
        assertTrue(c.metricsAvailable)
        assertEquals(2, c.nodes.size)
        assertEquals(5.5, c.totals.cpuFree, 1e-9)
        assertEquals(180, c.totals.podsFree)
        assertFalse(c.nPlusOne.ok)
        assertEquals("demo-worker-1", c.nPlusOne.node)
        assertEquals("demo/batch-importer", c.workloads.first().let { "${it.namespace}/${it.name}" })
        assertEquals(-1, c.workloads.first().pdbAllowed)
    }

    @Test
    fun nodesKnowWhetherPodsMayLand() {
        val c = TalosJson.decodeFromString(KubeCapacity.serializer(), capacityJson)
        assertTrue(c.nodes[0].schedulable)
        assertTrue(c.nodes[0].overcommitted)
        assertEquals(0.5f, c.nodes[0].memoryUsageFraction, 1e-6f)
        assertFalse(c.nodes[1].schedulable)
        // No metrics: no usage bar to fill.
        assertEquals(0f, c.nodes[1].memoryUsageFraction, 0f)
    }

    @Test
    fun unknownWordsFallBack() {
        val w = TalosJson.decodeFromString(KubeCapacity.serializer(), capacityJson).workloads
        assertEquals(QosClass.BEST_EFFORT, w[0].qosClass)
        assertEquals(CapacityRisk.EVICT_FIRST, w[0].riskLevel)
        assertEquals(CapacityRisk.OOM, w[1].riskLevel)
        // A newer core's words: shown as unknown QoS and no risk, never a crash.
        assertEquals(QosClass.UNKNOWN, w[3].qosClass)
        assertEquals(CapacityRisk.OK, w[3].riskLevel)
        assertFalse(w[3].atRisk)
    }

    @Test
    fun filtersKeepTheDeliveredOrder() {
        val w = TalosJson.decodeFromString(KubeCapacity.serializer(), capacityJson).workloads
        assertEquals(4, w.filteredBy(CapacityFilter.ALL).size)
        assertEquals(listOf("batch-importer", "report-cache", "kube-proxy"), w.filteredBy(CapacityFilter.AT_RISK).map { it.name })
        assertEquals(listOf("batch-importer"), w.filteredBy(CapacityFilter.NO_PRIORITY_CLASS).map { it.name })
    }

    @Test
    fun systemClassesByNameAndValue() {
        assertTrue(isSystemClass("system-cluster-critical", 0))
        assertTrue(isSystemClass("custom", 1_500_000_000))
        assertFalse(isSystemClass("web-high", 10000))
        assertTrue(TalosJson.decodeFromString(KubeCapacity.serializer(), capacityJson).workloads[2].isSystem)
    }

    @Test
    fun decodesANodeLossPlan() {
        val json = """{"node":"demo-worker-1","worstCase":true,"cpuLost":1.55,"memoryLost":1965031424,"podsLost":6,"daemonSetPods":1,
            "placed":[{"namespace":"demo","name":"postgres-0","workload":"StatefulSet/postgres","priority":10000,"to":"demo-worker-2"}],
            "preempted":[{"namespace":"demo","name":"worker-6f4b8-uvwxy","workload":"Deployment/worker","priority":0,
              "priorityClassName":"","node":"demo-worker-2","by":"demo/postgres-0","pdbViolated":false}],
            "pending":[{"namespace":"demo","name":"worker-6f4b8-uvwxy","workload":"Deployment/worker","priority":0,"qos":"Burstable","reason":"memory"}],
            "suggestions":[{"kind":"Deployment","namespace":"demo","name":"batch-importer","replicas":2,"scaleTo":0,"noPriorityClass":true,
              "frees":{"cpu":0,"memory":0,"pods":2}},
              {"kind":"StatefulSet","namespace":"demo","name":"postgres","replicas":1,"scaleTo":1,"pdb":"postgres","pdbMinAvailable":1,"pdbBlocks":true},
              {"kind":"CronJob","namespace":"demo","name":"nightly","replicas":1,"scaleTo":0}],
            "covered":false,"candidates":[{"name":"demo-worker-1","pending":2,"preempted":1},{"name":"demo-worker-2","pending":1}],
            "assumptions":["An estimate."]}"""
        val plan = TalosJson.decodeFromString(NodeLossPlan.serializer(), json)
        assertTrue(plan.worstCase)
        assertFalse(plan.covered)
        assertEquals("demo/postgres-0", plan.preempted.single().by)
        assertEquals("memory", plan.pending.single().reason)
        assertEquals(2, plan.candidates.size)
        assertTrue(plan.suggestions[1].pdbBlocks)

        // A suggestion opens the browser's scale dialog of its object; a kind it cannot scale, none.
        val ref = plan.suggestions[0].objectRef()!!
        assertEquals("apps" to "deployments", ref.group to ref.resource)
        assertTrue(ref.scalable)
        assertEquals("statefulsets", plan.suggestions[1].objectRef()!!.resource)
        assertNull(plan.suggestions[2].objectRef())
    }

    @Test
    fun freeFractionIsBounded() {
        assertEquals(0.25f, freeFraction(1.0, 4.0), 1e-6f)
        assertEquals(1f, freeFraction(5.0, 4.0), 0f)
        assertEquals(0f, freeFraction(1.0, 0.0), 0f)
    }
}

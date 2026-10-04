package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class WorkloadsTest {

    private val workloads = listOf(
        KubeWorkload("Deployment", "shop", "web", 3, 3, 3, 3, "ready", images = listOf("nginx:1.27")),
        KubeWorkload("StatefulSet", "shop", "db", 1, 0, 1, 0, "degraded", images = listOf("postgres:17")),
        KubeWorkload("DaemonSet", "kube-system", "proxy", 3, 3, 2, 3, "progressing"),
        KubeWorkload("Deployment", "a", "frozen", 1, 1, 1, 1, "paused"),
    )

    @Test
    fun decodesTheGoJson() {
        val json = """{"workloads":[{"kind":"Deployment","namespace":"shop","name":"web","desired":3,"ready":2,""" +
            """"updated":3,"available":2,"state":"degraded","restartedAt":1700000000000,"created":1,"images":["nginx:1.27"]}]}"""
        val w = TalosJson.decodeFromString(KubeWorkloadList.serializer(), json).workloads.single()
        assertEquals(WorkloadState.DEGRADED, w.workloadState)
        assertEquals(1_700_000_000_000L, w.restartedAt)
        assertEquals("Deployment/shop/web", w.key)
    }

    @Test
    fun decodesTheRolloutStatus() {
        val json = """{"workload":{"kind":"Deployment","namespace":"shop","name":"web","desired":2,"updated":1,"state":"progressing"},""" +
            """"done":false,"failed":true,"pods":[{"name":"web-new-a","status":"ContainerCreating","ready":0,"containers":1,"updated":true},""" +
            """{"name":"web-old-a","status":"Running","healthy":true,"ready":1,"containers":1,"restarts":2,"node":"w1","created":5}]}"""
        val st = TalosJson.decodeFromString(KubeRolloutStatus.serializer(), json)
        assertEquals(WorkloadState.PROGRESSING, st.workload.workloadState)
        assertFalse(st.done)
        assertTrue(st.failed)
        assertEquals(listOf(true, false), st.pods.map { it.updated })
        assertEquals(2, st.pods[1].restarts)
    }

    @Test
    fun attentionFirstThenByNamespaceAndName() {
        assertEquals(listOf("db", "proxy", "frozen", "web"), workloads.filtered(null, "").map { it.name })
    }

    @Test
    fun filtersByNamespaceAndQuery() {
        assertEquals(listOf("db", "web"), workloads.filtered("shop", "").map { it.name })
        assertEquals(listOf("db"), workloads.filtered(null, " POSTGRES ").map { it.name })
        assertEquals(listOf("proxy"), workloads.filtered(null, "daemonset").map { it.name })
        assertTrue(workloads.filtered("kube-system", "web").isEmpty())
    }

    @Test
    fun namespacesAndRestartability() {
        assertEquals(listOf("a", "kube-system", "shop"), workloads.namespaces)
        assertFalse(workloads.first { it.name == "frozen" }.canRestart)
        assertTrue(workloads.first { it.name == "web" }.canRestart)
        assertEquals(WorkloadState.UNKNOWN, WorkloadState.from("weird"))
    }

    @Test
    fun ownersOfAnAppsPodsThroughReplicaSetsAndDirectOwners() {
        val kubePods = listOf(
            KubePod("shop", "web-5d8f-abcde", owner = "ReplicaSet/web-5d8f"),
            KubePod("shop", "web-5d8f-fghij", owner = "ReplicaSet/web-5d8f"),
            KubePod("shop", "db-0", owner = "StatefulSet/db"),
            KubePod("kube-system", "proxy-xyz", owner = "DaemonSet/proxy"),
            KubePod("shop", "migrate-1-q", owner = "Job/migrate-1"),
            KubePod("kube-system", "apiserver-cp1", owner = "Node/cp1"),
        )
        val app = listOf(
            InventoryPod("shop", "web-5d8f-abcde"),
            InventoryPod("shop", "web-5d8f-fghij"),
            InventoryPod("shop", "db-0"),
            InventoryPod("shop", "migrate-1-q"),
        )
        assertEquals(listOf("web", "db"), workloads.ownersOf(app, kubePods).map { it.name })
        assertEquals(listOf("proxy"), workloads.ownersOf(listOf(InventoryPod("kube-system", "proxy-xyz")), kubePods).map { it.name })
    }

    @Test
    fun ownersOfIgnoresUnknownPodsOtherNamespacesAndOddOwners() {
        val kubePods = listOf(
            KubePod("other", "web-5d8f-abcde", owner = "ReplicaSet/web-5d8f"),
            KubePod("shop", "bare", owner = ""),
            KubePod("shop", "odd", owner = "ReplicaSet/nohash"),
        )
        val app = listOf(InventoryPod("other", "web-5d8f-abcde"), InventoryPod("shop", "bare"), InventoryPod("shop", "odd"), InventoryPod("shop", "gone"))
        assertTrue(workloads.ownersOf(app, kubePods).isEmpty())
    }
}

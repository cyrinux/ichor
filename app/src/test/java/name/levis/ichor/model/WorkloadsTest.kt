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
}

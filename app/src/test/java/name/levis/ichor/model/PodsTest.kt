package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class PodsTest {

    private val pods = listOf(
        KubePod("shop", "web-1", "Running", healthy = true, ready = 1, containers = 1, node = "w1", owner = "ReplicaSet/web-5d8f", images = listOf("nginx:1.27")),
        KubePod("shop", "worker-1", "CrashLoopBackOff", ready = 0, containers = 1, restarts = 14, node = "w2"),
        KubePod("kube-system", "coredns-1", "Running", healthy = true, ready = 1, containers = 1, node = "cp1"),
        KubePod("a", "job-1", "Pending"),
    )

    @Test
    fun decodesTheGoJson() {
        val json = """{"pods":[{"namespace":"shop","name":"web-1","status":"Init:1/2","healthy":false,"ready":0,""" +
            """"containers":2,"restarts":3,"node":"w1","owner":"ReplicaSet/web","created":1,"images":["nginx"]}]}"""
        val p = TalosJson.decodeFromString(KubePodList.serializer(), json).pods.single()
        assertEquals(3, p.restarts)
        assertEquals("shop/web-1", p.key)
        assertTrue(p.transitional)
    }

    @Test
    fun unhealthyFirstThenNamespaceAndName() {
        assertEquals(listOf("job-1", "worker-1", "coredns-1", "web-1"), pods.filteredPods(null, "").map { it.name })
    }

    @Test
    fun filtersByNamespaceStatusNodeOwnerOrImage() {
        assertEquals(listOf("worker-1", "web-1"), pods.filteredPods("shop", "").map { it.name })
        assertEquals(listOf("worker-1"), pods.filteredPods(null, "crashloop").map { it.name })
        assertEquals(listOf("coredns-1"), pods.filteredPods(null, "CP1").map { it.name })
        assertEquals(listOf("web-1"), pods.filteredPods(null, "replicaset").map { it.name })
        assertEquals(listOf("web-1"), pods.filteredPods(null, "nginx").map { it.name })
        assertEquals(listOf("a", "kube-system", "shop"), pods.podNamespaces)
    }

    @Test
    fun transitionalStates() {
        assertTrue(KubePod("a", "b", "Terminating").transitional)
        assertFalse(KubePod("a", "b", "Init:Error").transitional)
        assertFalse(KubePod("a", "b", "CrashLoopBackOff").transitional)
    }
}

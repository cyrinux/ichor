package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class KubeTopTest {

    @Test
    fun decodesTheCoreJson() {
        val json = """{"available":true,"forbidden":false,"pods":[{"namespace":"web","name":"api","node":"w1","cpu":0.125,"memory":230686720,
            "cpuRequest":0.3,"cpuLimit":1.1,"memoryRequest":335544320,"memoryLimit":671088640}]}"""
        val top = TalosJson.decodeFromString(KubeTopPods.serializer(), json)
        assertTrue(top.available)
        // An older core says nothing: not read, so no bar is drawn against bounds it never sent.
        assertFalse(top.boundsRead)
        assertTrue(TalosJson.decodeFromString(KubeTopPods.serializer(), """{"available":true,"boundsRead":true}""").boundsRead)
        assertEquals(0.125, top.pods.single().cpu, 1e-9)
        assertEquals("web/api", top.pods.single().key)
    }

    @Test
    fun decodesAPodWithItsContainers() {
        val json = """{"available":true,"boundsRead":true,"pods":[{"namespace":"web","name":"api","cpu":0.125,"memory":230686720,
            "containers":[{"name":"app","cpu":0.12,"memory":209715200,"cpuRequest":0.25,"cpuLimit":1,"memoryRequest":268435456,"memoryLimit":0},
            {"name":"proxy","cpu":0.005,"memory":20971520,"cpuRequest":0,"cpuLimit":0,"memoryRequest":0,"memoryLimit":0}]}]}"""
        val pod = TalosJson.decodeFromString(KubeTopPods.serializer(), json).pods.single()
        assertEquals(listOf("app", "proxy"), pod.containers.map { it.name })
        val app = pod.containers.first()
        assertEquals(0.12f, app.cpuFraction()!!, 1e-6f)
        // No memory limit: against its own request.
        assertEquals(0.78125f, app.memoryFraction()!!, 1e-6f)
        // Neither bound: no bar.
        assertNull(pod.containers.last().cpuFraction())
        // An older core sends no containers: an empty list, not a failure.
        assertTrue(TalosJson.decodeFromString(KubeTopPod.serializer(), """{"name":"api"}""").containers.isEmpty())
    }

    @Test
    fun aPodBarIsMeasuredAgainstItsLimitElseItsRequest() {
        val bounded = KubeTopPod(cpu = 0.5, cpuRequest = 0.25, cpuLimit = 1.0, memory = 300.0, memoryRequest = 200.0)
        assertEquals(0.5f, bounded.cpuFraction()!!, 1e-6f)
        // No memory limit: against the request, capped at a full bar.
        assertEquals(1f, bounded.memoryFraction()!!, 1e-6f)
        // Neither: no bar, only the figure.
        assertNull(KubeTopPod(cpu = 0.1).cpuFraction())
    }

    @Test
    fun cpuReadsLikeKubectl() {
        assertEquals("0m", formatCpu(0.0))
        assertEquals("125m", formatCpu(0.125))
        assertEquals("999m", formatCpu(0.9994))
        assertEquals("1.5", formatCpu(1.5))
        assertEquals("12", formatCpu(12.0))
    }

    @Test
    fun sortsBusiestFirst() {
        val pods = listOf(
            KubeTopPod(namespace = "a", name = "idle", cpu = 0.01, memory = 900.0),
            KubeTopPod(namespace = "a", name = "busy", cpu = 2.0, memory = 10.0),
        )
        assertEquals(listOf("busy", "idle"), pods.sortedByUsage(TopSort.CPU) { it }.map { it.name })
        assertEquals(listOf("idle", "busy"), pods.sortedByUsage(TopSort.MEMORY) { it }.map { it.name })
        // A row with no metrics (pending, just started) goes last.
        val rows = listOf("new", "busy")
        assertEquals(listOf("busy", "new"), rows.sortedByUsage(TopSort.CPU) { name -> pods.firstOrNull { it.name == name } }.toList())
    }

    @Test
    fun forbiddenIsNotAvailable() {
        val top = TalosJson.decodeFromString(KubeTopNodes.serializer(), """{"available":false,"forbidden":true,"nodes":[]}""")
        assertFalse(top.available)
        assertTrue(top.forbidden)
        assertTrue(top.byName.isEmpty())
    }
}

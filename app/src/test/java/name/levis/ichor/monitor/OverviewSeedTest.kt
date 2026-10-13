package name.levis.ichor.monitor

import name.levis.ichor.data.MemoryPrefs
import name.levis.ichor.data.MemoryValue
import name.levis.ichor.data.OfflineCache
import name.levis.ichor.model.NodeHealth
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class OverviewSeedTest {

    private val now = 100_000_000L

    private val talos = ClusterSnapshot(
        context = "lab",
        takenAt = now - 60_000,
        fingerprint = "fp",
        nodes = mapOf(
            "192.0.2.12" to NodeState("worker-2", NodeHealth.UNREACHABLE, "connection refused"),
            "192.0.2.10" to NodeState("cp-1", NodeHealth.READY),
            "192.0.2.11" to NodeState("worker-1", NodeHealth.NOT_READY, "Ready: KubeletNotReady"),
        ),
    )

    private val kube = ClusterSnapshot(
        context = "cloud",
        takenAt = now - 60_000,
        fingerprint = "fk",
        kube = true,
        nodes = mapOf(
            "node-b" to NodeState("node-b", NodeHealth.NOT_READY, "MemoryPressure; DiskPressure"),
            "node-a" to NodeState("node-a", NodeHealth.READY),
        ),
    )

    @Test
    fun theOverviewSeedHasEachNodeAndItsHealth() {
        val seed = talos.toOverviewSeed()
        assertEquals("lab", seed.context)
        assertEquals(listOf("cp-1", "worker-1", "worker-2"), seed.nodes.map { it.hostname })
        val (cp, notReady, down) = seed.nodes
        assertTrue(cp.reachable && cp.ready)
        assertTrue(notReady.reachable && !notReady.ready)
        assertNull(notReady.error)
        assertFalse(down.reachable)
        assertEquals("connection refused", down.error)
        assertEquals("192.0.2.12", down.node)
        // What the check does not keep stays unknown.
        assertEquals("", cp.version)
        assertEquals(0, cp.cpuCount)
    }

    @Test
    fun theKubeSeedHasEachNodeAndItsPressure() {
        val seed = kube.toKubeNodesSeed()
        assertEquals(listOf("node-a", "node-b"), seed.nodes.map { it.name })
        assertTrue(seed.nodes[0].ready && seed.nodes[0].pressure.isEmpty())
        assertEquals(listOf("MemoryPressure", "DiskPressure"), seed.nodes[1].pressure)
    }

    @Test
    fun aSeedOnlyFromARecentCheckOfTheRightKindOutsideScreenshotMode() {
        val store = MonitorStore(MemoryPrefs(), MemoryValue())
        store.saveState(MonitorState(mapOf("fp" to talos, "fk" to kube, "empty" to talos.copy(nodes = emptyMap()))))

        val seeded = store.seed("fp", kube = false, masked = false, now = now) { it.toOverviewSeed() }
        assertEquals(talos.takenAt, seeded?.at)
        assertEquals(3, seeded?.value?.nodes?.size)
        assertEquals(2, store.seed("fk", kube = true, masked = false, now = now) { it.toKubeNodesSeed() }?.value?.nodes?.size)

        // The demo (no fingerprint), screenshot mode, the other kind, no node, an old check, an unknown cluster.
        assertNull(store.seed(null, kube = false, masked = false, now = now) { it })
        assertNull(store.seed("fp", kube = false, masked = true, now = now) { it })
        assertNull(store.seed("fp", kube = true, masked = false, now = now) { it })
        assertNull(store.seed("empty", kube = false, masked = false, now = now) { it })
        assertNull(store.seed("fp", kube = false, masked = false, now = talos.takenAt + OfflineCache.MAX_AGE_MS + 1) { it })
        assertNull(store.seed("other", kube = false, masked = false, now = now) { it })
    }
}

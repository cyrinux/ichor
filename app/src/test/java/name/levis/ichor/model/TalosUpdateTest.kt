package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class TalosUpdateTest {

    private fun node(name: String, version: String, reachable: Boolean = true, role: String = "worker") =
        NodeOverview(node = "192.0.2.$name", hostname = name, reachable = reachable, version = version, role = role)

    @Test
    fun decodes() {
        val c = TalosJson.decodeFromString(
            TalosUpdateCheck.serializer(),
            """{"latest":"v1.14.2","latestDate":"2026-09-30T10:00:00Z","newer":true,"outdated":2,"oldest":"v1.13.5","notes":"https://github.com/siderolabs/talos/releases/tag/v1.14.2"}""",
        )
        assertTrue(c.newer)
        assertEquals(2, c.outdated)
    }

    @Test
    fun versionsCsv() {
        val nodes = listOf(node("1", "v1.14.1"), node("2", "v1.13.5"), node("3", "v1.14.1"), node("4", "v1.12.0", reachable = false), node("5", ""))
        assertEquals("v1.13.5,v1.14.1", nodeVersionsCsv(nodes))
    }

    @Test
    fun olderVersions() {
        assertTrue(isOlderVersion("v1.13.9", "v1.14.0"))
        assertTrue(isOlderVersion("v1.14.1", "v1.14.10"))
        assertTrue(isOlderVersion("v1.14.2-beta.1", "v1.14.2"))
        assertFalse(isOlderVersion("v1.14.2", "v1.14.2"))
        assertFalse(isOlderVersion("v1.15.0", "v1.14.2"))
        assertFalse(isOlderVersion("garbage", "v1.14.2"))
    }

    @Test
    fun outdatedOldestFirst() {
        val nodes = listOf(node("b", "v1.14.1"), node("a", "v1.13.5"), node("c", "v1.14.2"), node("d", "v1.13.0", reachable = false), node("e", "v1.14.1"))
        assertEquals(listOf("a", "b", "e"), outdatedNodes(nodes, "v1.14.2").map { it.hostname })
    }

    @Test
    fun choicesByRole() {
        val nodes = listOf(
            node("w2", "v1.14.1"),
            node("w1", "v1.13.5"),
            node("cp2", "v1.14.1", role = "controlplane"),
            node("cp1", "v1.14.1", role = "controlplane"),
            node("cp3", "v1.14.2", role = "controlplane"),
            node("x", "v1.14.1", role = "unknown"),
        )
        val choices = upgradeChoices(nodes, "v1.14.2")
        assertEquals(listOf("cp1", "cp2"), choices.controlPlane.map { it.hostname })
        assertEquals(listOf("w1", "w2", "x"), choices.workers.map { it.hostname })
        assertEquals(5, choices.count)
        assertTrue(choices.workersWait)

        val workersOnly = upgradeChoices(nodes.filter { it.role != "controlplane" }, "v1.14.2")
        assertTrue(workersOnly.controlPlane.isEmpty())
        assertFalse(workersOnly.workersWait)
    }

    @Test
    fun freshness() {
        assertTrue(talosUpdateFresh(1_000, "v1", "v1", 1_000 + TALOS_UPDATE_INTERVAL_MILLIS - 1))
        assertFalse(talosUpdateFresh(1_000, "v1", "v1", 1_000 + TALOS_UPDATE_INTERVAL_MILLIS))
        assertFalse(talosUpdateFresh(1_000, "v1", "v2", 1_001))
        assertFalse(talosUpdateFresh(0, null, "v1", 1))
    }
}

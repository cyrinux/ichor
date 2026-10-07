package name.levis.ichor.data

import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.KIND_KUBE
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class StoredConfigTest {

    private val talos = ConfigSummary("admin@prod", listOf(ContextSummary("admin@prod"), ContextSummary("admin@lab")))
    private val kube = ConfigSummary("eks", listOf(ContextSummary("eks", kind = KIND_KUBE), ContextSummary("k3s", kind = KIND_KUBE)))
    private val merged = mergeSummaries(talos, kube)

    private fun stored(active: String) = StoredConfig("talos-yaml", "kube-yaml", merged, active)

    @Test
    fun talosContextsComeFirst() {
        assertEquals(listOf("admin@prod", "admin@lab", "eks", "k3s"), merged.contexts.map { it.name })
        assertEquals("admin@prod", merged.current)
        assertEquals(2, talosContextCount(merged))
    }

    @Test
    fun currentFallsBackToTheKubeconfig() {
        assertEquals("eks", mergeSummaries(null, kube).current)
        assertEquals("admin@prod", mergeSummaries(talos, null).current)
        assertEquals(ConfigSummary("", emptyList()), mergeSummaries(null, null))
    }

    @Test
    fun yamlFollowsTheActiveContext() {
        assertEquals("talos-yaml", stored("admin@lab").yaml)
        assertEquals("kube-yaml", stored("k3s").yaml)
        assertTrue(stored("eks").activeIsKube)
        assertFalse(stored("admin@prod").activeIsKube)
    }

    @Test
    fun yamlForAnotherContext() {
        // A debug shell or an endpoint probe names its own context, not the one on screen.
        val onKube = stored("eks")
        assertEquals("talos-yaml", onKube.yamlFor("admin@lab"))
        assertEquals("kube-yaml", onKube.yamlFor("k3s"))
        // Unknown: the talosconfig, as before kubeconfig clusters existed.
        assertEquals("talos-yaml", onKube.yamlFor("gone"))
    }

    @Test
    fun activeResolvesAcrossBothStores() {
        // The saved position covers the merged list, kubeconfig contexts included.
        assertEquals("k3s", resolveActive(merged, "masked", 3))
        assertEquals("eks", resolveActive(merged, "eks", -1))
        assertEquals("admin@lab", adjacentContext(merged, "eks", -1))
    }

    @Test
    fun noCredentialsInToString() {
        assertFalse("yaml" in stored("eks").toString())
    }
}

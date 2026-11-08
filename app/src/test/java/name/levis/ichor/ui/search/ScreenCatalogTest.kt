package name.levis.ichor.ui.search

import name.levis.ichor.R
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.KIND_KUBE
import name.levis.ichor.model.NodeOverview
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ScreenCatalogTest {
    private val admin = ContextSummary(name = "admin@lab", roles = listOf("os:admin"))
    private val reader = ContextSummary(name = "reader@lab", roles = listOf("os:reader"))
    private val omni = ContextSummary(name = "omni-lab", omni = true)
    private val kube = ContextSummary(name = "kind-dev", kind = KIND_KUBE)
    private val nodes = listOf(
        NodeOverview(node = "10.0.0.1", hostname = "node-a", reachable = true),
        NodeOverview(node = "10.0.0.2", hostname = "node-b", reachable = false),
    )
    private val talosOnly = listOf("etcd", "events", "kubespan", "join-node", "upgrade-cluster", "upgrade-kubernetes")

    private fun ids(cluster: ContextSummary) = screenCatalog(cluster, nodes).map { it.id }

    @Test
    fun aKubeconfigClusterHasNoTalosScreen() {
        val ids = ids(kube)
        talosOnly.forEach { assertFalse(it, it in ids) }
        assertFalse(ids.any { it.startsWith("node/") })
        assertTrue("workloads" in ids && "checkup" in ids && "runbooks" in ids)
    }

    @Test
    fun aTalosAdminHasTheOperationsAndEveryNode() {
        val ids = ids(admin)
        talosOnly.forEach { assertTrue(it, it in ids) }
        assertTrue("node/10.0.0.1/hardware" in ids && "node/10.0.0.2/hardware" in ids)
        assertTrue("node/10.0.0.1/debug_shell" in ids)
    }

    @Test
    fun aReaderCannotUpgradeNorOpenAdminNodeScreens() {
        val ids = ids(reader)
        assertFalse("upgrade-cluster" in ids || "upgrade-kubernetes" in ids || "health" in ids)
        assertFalse("node/10.0.0.1/debug_shell" in ids)
        assertTrue("node/10.0.0.1/hardware" in ids && "etcd" in ids)
    }

    @Test
    fun omniAddsANodeThroughTheBootMediaAsTheMenuDoes() {
        assertTrue("join-node" in ids(omni))
    }

    @Test
    fun noNodeScreenBeforeTheFirstOverview() {
        assertFalse(screenCatalog(admin).any { it.id.startsWith("node/") })
    }

    @Test
    fun theTabsAndConfirmationsAreNotScreens() {
        val ids = ids(admin)
        listOf("tab_services", "reset", "cordon", "captures").forEach { assertFalse(it, "node/10.0.0.1/$it" in ids) }
    }

    @Test
    fun idsAreUnique() {
        val ids = ids(admin)
        assertEquals(ids.size, ids.toSet().size)
    }

    @Test
    fun titlesNameTheirNodeOrSettingsWithoutAnEllipsis() {
        val text: (Int) -> String = { res ->
            when (res) {
                R.string.settings_title -> "Settings"
                R.string.monitor_section -> "Alerts"
                R.string.node_menu_hardware -> "Hardware"
                R.string.join_node_menu -> "Add a node…"
                else -> "x"
            }
        }
        val titles = screenEntries(screenCatalog(admin, nodes), text).associate { it.id to it.title }
        assertEquals("Settings → Alerts", titles["settings/alerts"])
        assertEquals("node-a → Hardware", titles["node/10.0.0.1/hardware"])
        assertEquals("Add a node", titles["join-node"])
        assertEquals("etcd", titles["etcd"])
    }
}

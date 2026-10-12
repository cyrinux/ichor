package name.levis.ichor.ui.node

import name.levis.ichor.R
import name.levis.ichor.model.ContextSummary
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Test

class NodeMenuTest {

    private val admin = ContextSummary(name = "a", roles = listOf("os:admin"))

    @Test
    fun anAdminGetsEveryGroupInScreenOrder() {
        val groups = nodeMenuGroups(admin)
        assertEquals(NodeMenuGroup.entries, groups.map { it.first })
        assertEquals(NodeMenuEntry.entries, groups.flatMap { it.second })
    }

    @Test
    fun statusComesFirstWithEveryTabForAnAdmin() {
        val (group, entries) = nodeMenuGroups(admin).first()
        assertEquals(NodeMenuGroup.STATUS, group)
        assertEquals(listOf(0, 1, 2, 3, 4, CGROUPS_TAB, KUBE_PODS_TAB), entries.map { it.tab })
        assertEquals(nodeTabs(canCgroups = true, canKubePods = true), entries.map { it.tab })
    }

    @Test
    fun aGroupTheRoleLeavesEmptyIsNotShown() {
        val groups = nodeMenuGroups(null)
        assertEquals(listOf(NodeMenuGroup.STATUS, NodeMenuGroup.INSPECT), groups.map { it.first })
        assertEquals(
            listOf(
                NodeMenuEntry.TAB_SERVICES, NodeMenuEntry.TAB_RESOURCES, NodeMenuEntry.TAB_LIVE,
                NodeMenuEntry.TAB_PROCESSES, NodeMenuEntry.TAB_CONTAINERS,
            ),
            groups[0].second,
        )
        assertEquals(
            listOf(
                NodeMenuEntry.KERNEL_LOG, NodeMenuEntry.EVENTS, NodeMenuEntry.NETWORK,
                NodeMenuEntry.HARDWARE, NodeMenuEntry.IMAGES, NodeMenuEntry.STORAGE,
            ),
            groups[1].second,
        )
    }

    @Test
    fun onlyStatusEntriesOpenATab() {
        NodeMenuEntry.entries.forEach { entry ->
            if (entry.group == NodeMenuGroup.STATUS) assertNotNull(entry.name, entry.tab) else assertNull(entry.name, entry.tab)
        }
    }

    @Test
    fun everyEntryStaysWithItsGroup() {
        nodeMenuGroups(admin).forEach { (group, entries) ->
            assertEquals(entries.map { group }, entries.map { it.group })
        }
    }

    @Test
    fun labelsFollowTheNodeAndTheTabsNextToThem() {
        assertEquals(R.string.node_menu_uncordon, NodeMenuEntry.CORDON.label(cordoned = true, kubePods = false))
        assertEquals(R.string.node_menu_cordon, NodeMenuEntry.CORDON.label(cordoned = null, kubePods = false))
        assertEquals(R.string.apps_stat_containers, NodeMenuEntry.TAB_CONTAINERS.label(cordoned = null, kubePods = true))
        assertEquals(R.string.node_tab_pods, NodeMenuEntry.TAB_CONTAINERS.label(cordoned = null, kubePods = false))
    }
}

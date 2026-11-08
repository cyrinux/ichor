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
    fun railIndicesFollowTheItemsNodeSectionsEmits() {
        // After the header (0): Status title + tiles, Inspect title + tiles, then one row per entry.
        val groups = nodeMenuGroups(admin)
        val jumps = nodeJumpSections(groups, power = true)
        var index = 1
        val expected = groups.map { (group, entries) ->
            NodeJump(group.name, group.title, index).also {
                index += 1 + if (group == NodeMenuGroup.STATUS || group == NodeMenuGroup.INSPECT) 1 else entries.size
            }
        } + NodeJump("power", R.string.node_menu_group_power, index)
        assertEquals(expected, jumps)
        assertEquals(listOf(1, 3, 5), jumps.take(3).map { it.index })
    }

    @Test
    fun aReaderGetsNoPowerDot() {
        val jumps = nodeJumpSections(nodeMenuGroups(null), power = false)
        assertEquals(listOf(NodeMenuGroup.STATUS.name, NodeMenuGroup.INSPECT.name), jumps.map { it.key })
        assertEquals(listOf(1, 3), jumps.map { it.index })
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

    @Test
    fun onlyTheKubernetesEntriesAndSavedCapturesSkipTheTalosApi() {
        val byTransport = NodeMenuEntry.entries.groupBy { it.transport }
        assertEquals(
            listOf(
                NodeMenuEntry.TAB_KUBE_PODS, NodeMenuEntry.MAINTENANCE, NodeMenuEntry.DRAIN,
                NodeMenuEntry.CORDON, NodeMenuEntry.TAINTS_LABELS,
            ),
            byTransport[NodeTransport.KUBE],
        )
        assertEquals(listOf(NodeMenuEntry.CAPTURES), byTransport[NodeTransport.LOCAL])
        assertEquals(NodeMenuEntry.entries.size - 6, byTransport[NodeTransport.TALOS]?.size)
    }

    @Test
    fun theWakeRowAtTheEndOfOperatePushesPowerDownByOne() {
        val groups = nodeMenuGroups(admin)
        val without = nodeJumpSections(groups, power = true)
        val with = nodeJumpSections(groups, power = true, wake = true)
        assertEquals(without.dropLast(1), with.dropLast(1))
        assertEquals(without.last().index + 1, with.last().index)
    }
}

package name.levis.ichor.ui.node

import name.levis.ichor.model.ContextSummary
import org.junit.Assert.assertEquals
import org.junit.Test

class NodeMenuTest {

    private val admin = ContextSummary(name = "a", roles = listOf("os:admin"))

    @Test
    fun anAdminGetsEveryGroupInMenuOrder() {
        val groups = nodeMenuGroups(admin)
        assertEquals(NodeMenuGroup.entries, groups.map { it.first })
        assertEquals(NodeMenuEntry.entries, groups.flatMap { it.second })
    }

    @Test
    fun aGroupTheRoleLeavesEmptyIsNotShown() {
        val groups = nodeMenuGroups(null)
        assertEquals(listOf(NodeMenuGroup.INSPECT), groups.map { it.first })
        assertEquals(
            listOf(
                NodeMenuEntry.KERNEL_LOG, NodeMenuEntry.EVENTS, NodeMenuEntry.NETWORK,
                NodeMenuEntry.HARDWARE, NodeMenuEntry.IMAGES, NodeMenuEntry.STORAGE,
            ),
            groups.single().second,
        )
    }

    @Test
    fun everyEntryStaysWithItsGroup() {
        nodeMenuGroups(admin).forEach { (group, entries) ->
            assertEquals(entries.map { group }, entries.map { it.group })
        }
    }
}

package name.levis.ichor.data

import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.EtcdNodeStatus
import name.levis.ichor.model.Feature
import name.levis.ichor.model.allows
import name.levis.ichor.model.defragOrder
import name.levis.ichor.model.reclaimable
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class DefragTest {

    private fun member(node: String, leader: Boolean = false, size: Long = 100, inUse: Long = 40, error: String? = null) =
        EtcdNodeStatus(node = node, memberId = if (error == null) "id-$node" else "", isLeader = leader, dbSize = size, dbSizeInUse = inUse, error = error)

    @Test
    fun followersFirstLeaderLastSkippingUnreachable() {
        val order = defragOrder(
            listOf(
                member("leader", leader = true, size = 900),
                member("small", size = 100, inUse = 90),
                member("down", error = "timed out"),
                member("big", size = 500, inUse = 50),
            ),
        )
        assertEquals(listOf("big", "small", "leader"), order.map { it.node })
    }

    @Test
    fun reclaimableNeverNegative() {
        assertEquals(60L, member("a").reclaimable)
        assertEquals(0L, member("b", size = 10, inUse = 20).reclaimable)
    }

    @Test
    fun operatorAndAdminCanDefragReaderCannot() {
        assertTrue(ContextSummary(name = "o", roles = listOf("os:operator")).allows(Feature.ETCD_DEFRAG))
        assertTrue(ContextSummary(name = "a", roles = listOf("os:admin")).allows(Feature.ETCD_DEFRAG))
        assertFalse(ContextSummary(name = "r", roles = listOf("os:reader")).allows(Feature.ETCD_DEFRAG))
    }
}

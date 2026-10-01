package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class EtcdMembersTest {

    private val statuses = listOf(
        EtcdNodeStatus(node = "10.0.0.1", memberId = "a", isLeader = true),
        EtcdNodeStatus(node = "10.0.0.2", memberId = "b"),
        EtcdNodeStatus(node = "10.0.0.3", error = "unreachable"),
    )

    @Test
    fun removalGoesThroughAnotherHealthyFollowerFirst() {
        assertEquals("10.0.0.2", removalNode(statuses, "c"))
        // Removing the only follower: the leader takes the request.
        assertEquals("10.0.0.1", removalNode(statuses, "b"))
        assertEquals("10.0.0.2", removalNode(statuses, "a"))
    }

    @Test
    fun removalPrefersAMemberWithoutEtcdErrors() {
        val withErrors = listOf(
            EtcdNodeStatus(node = "10.0.0.2", memberId = "b", errors = listOf("corrupt")),
            EtcdNodeStatus(node = "10.0.0.1", memberId = "a", isLeader = true),
        )
        assertEquals("10.0.0.1", removalNode(withErrors, "c"))
        // Still better than nobody.
        assertEquals("10.0.0.2", removalNode(withErrors, "a"))
    }

    @Test
    fun noRemovalNodeWhenNobodyElseAnswers() {
        assertNull(removalNode(listOf(statuses[0]), "a"))
        assertNull(removalNode(listOf(statuses[2]), "a"))
        assertNull(removalNode(emptyList(), "a"))
    }

    @Test
    fun decodesPlan() {
        val json = """{"member":{"id":"b","hostname":"cp-2"},"healthyAfter":2,"membersAfter":2,"quorumAfter":true,"blockers":null,"warnings":["cp-2 is healthy"]}"""
        val plan = TalosJson.decodeFromString(EtcdMemberPlan.serializer(), json)
        assertEquals("cp-2", plan.member.hostname)
        assertTrue(plan.allowed)
        assertTrue(plan.quorumAfter)
        assertEquals(listOf("cp-2 is healthy"), plan.warnings)
    }

    @Test
    fun blockersForbid() {
        val plan = EtcdMemberPlan(healthyAfter = 1, membersAfter = 2, quorumAfter = false, blockers = listOf("quorum would be lost"))
        assertFalse(plan.allowed)
        assertFalse(plan.quorumAfter)
        assertTrue(EtcdMemberPlan().allowed)
    }
}

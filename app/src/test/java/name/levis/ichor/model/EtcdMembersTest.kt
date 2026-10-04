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

    private val members = listOf(
        EtcdMember(id = "a", hostname = "cp-1", peerUrls = listOf("https://10.0.0.1:2380")),
        EtcdMember(id = "b", hostname = "cp-2", clientUrls = listOf("https://[fd00::2]:2379")),
        EtcdMember(id = "c", hostname = "", peerUrls = listOf("https://10.0.0.4:2380")),
    )

    @Test
    fun namesANodeByItsMemberId() {
        val etcd = EtcdOverview(members = members, statuses = listOf(EtcdNodeStatus(node = "10.9.9.9", memberId = "a")))
        assertEquals("cp-1", etcd.nodeHostnames()["10.9.9.9"])
    }

    @Test
    fun namesAFailedNodeByTheAddressInItsMemberUrls() {
        val etcd = EtcdOverview(
            members = members,
            statuses = listOf(
                EtcdNodeStatus(node = "10.0.0.1", error = "unreachable"),
                EtcdNodeStatus(node = "fd00::2", error = "unreachable"),
                EtcdNodeStatus(node = "[fd00::2]", error = "unreachable"),
            ),
        )
        assertEquals(mapOf("10.0.0.1" to "cp-1", "fd00::2" to "cp-2", "[fd00::2]" to "cp-2"), etcd.nodeHostnames())
    }

    @Test
    fun keepsTheAddressWhenNoMemberNamesIt() {
        val etcd = EtcdOverview(
            members = members,
            statuses = listOf(
                EtcdNodeStatus(node = "10.0.0.3", error = "unreachable"),
                // A member without a hostname does not blank the address.
                EtcdNodeStatus(node = "10.0.0.4", memberId = "c"),
            ),
        )
        assertEquals(mapOf("10.0.0.3" to "10.0.0.3", "10.0.0.4" to "10.0.0.4"), etcd.nodeHostnames())
    }

    @Test
    fun fallsBackToHostnamesKnownElsewhere() {
        val etcd = EtcdOverview(
            members = members,
            statuses = listOf(
                EtcdNodeStatus(node = "10.0.0.1", error = "unreachable"),
                EtcdNodeStatus(node = "10.0.0.3", error = "unreachable"),
                EtcdNodeStatus(node = "10.0.0.5", error = "unreachable"),
            ),
        )
        val known = mapOf("10.0.0.1" to "stale", "10.0.0.3" to "cp-3", "10.0.0.5" to "")
        // etcd's own name wins; a blank one is no name.
        assertEquals(mapOf("10.0.0.1" to "cp-1", "10.0.0.3" to "cp-3", "10.0.0.5" to "10.0.0.5"), etcd.nodeHostnames(known))
    }
}

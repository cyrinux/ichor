package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class EtcdLagTest {

    private val leader = EtcdNodeStatus(node = "10.0.0.1", memberId = "a", isLeader = true, raftIndex = 10_000, raftAppliedIndex = 10_000)
    private val inSync = EtcdNodeStatus(node = "10.0.0.2", memberId = "b", raftIndex = 9_990, raftAppliedIndex = 9_990)
    private val behind = EtcdNodeStatus(node = "10.0.0.3", memberId = "c", raftIndex = 7_000, raftAppliedIndex = 7_000)
    private val statuses = listOf(leader, inSync, behind)

    @Test
    fun followerCloseToTheLeaderIsNotLagging() {
        val lag = etcdLag(inSync, statuses)!!
        assertEquals(10L, lag.behindLeader)
        assertEquals(0L, lag.applyBacklog)
        assertFalse(lag.lagging)
    }

    @Test
    fun followerFarBehindTheLeaderIsLagging() {
        val lag = etcdLag(behind, statuses)!!
        assertEquals(3_000L, lag.behindLeader)
        assertTrue(lag.lagging)
        assertEquals(listOf(behind), laggingMembers(statuses))
    }

    @Test
    fun theLeaderIsNeverBehindItself() {
        assertEquals(0L, etcdLag(leader, statuses)!!.behindLeader)
    }

    @Test
    fun aFollowerAheadOfTheLeaderProbeIsNotBehind() {
        // Probes are not simultaneous: a follower read later can be ahead of the leader read.
        val ahead = inSync.copy(raftIndex = 10_050, raftAppliedIndex = 10_050)
        assertEquals(0L, etcdLag(ahead, statuses)!!.behindLeader)
    }

    @Test
    fun applyBacklogAloneMakesAMemberLag() {
        val slowApply = inSync.copy(raftIndex = 10_000, raftAppliedIndex = 8_000)
        val lag = etcdLag(slowApply, statuses)!!
        assertEquals(2_000L, lag.applyBacklog)
        assertTrue(lag.lagging)
        // The leader can be slow to apply too.
        assertTrue(etcdLag(leader.copy(raftAppliedIndex = 5_000), statuses)!!.lagging)
    }

    @Test
    fun unknownAppliedIndexIsNoBacklog() {
        // Older cores do not report the applied index.
        assertEquals(0L, etcdLag(inSync.copy(raftAppliedIndex = 0), statuses)!!.applyBacklog)
    }

    @Test
    fun withoutAProbedLeaderTheGapIsUnknown() {
        val lag = etcdLag(inSync, listOf(inSync, behind))!!
        assertNull(lag.behindLeader)
        assertFalse(lag.lagging)
    }

    @Test
    fun unreachableMemberHasNoLag() {
        assertNull(etcdLag(EtcdNodeStatus(node = "10.0.0.4", error = "down"), statuses))
        val erroredLeader = leader.copy(error = "down")
        assertNull(etcdLag(inSync, listOf(erroredLeader, inSync))!!.behindLeader)
    }

    @Test
    fun decodesAppliedIndex() {
        val json = """{"node":"10.0.0.1","raftIndex":12,"raftAppliedIndex":11}"""
        assertEquals(11L, TalosJson.decodeFromString(EtcdNodeStatus.serializer(), json).raftAppliedIndex)
        assertEquals(0L, TalosJson.decodeFromString(EtcdNodeStatus.serializer(), """{"node":"n"}""").raftAppliedIndex)
    }
}

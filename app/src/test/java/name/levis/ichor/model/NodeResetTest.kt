package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class NodeResetTest {

    @Test
    fun decodesThePlanOfGo() {
        val plan = TalosJson.decodeFromString(
            NodeResetPlan.serializer(),
            """{"node":"10.0.0.1","hostname":"cp-1","role":"controlplane","etcdMember":{"id":"a1","healthy":true},""" +
                """"lastControlPlane":true,"blockers":["it is the only control plane"],"warnings":[],"userDisks":["/dev/sdb"]}""",
        )
        assertEquals("cp-1", plan.hostname)
        assertEquals(NodeResetMember("a1", true), plan.etcdMember)
        assertTrue(plan.lastControlPlane)
        assertFalse(plan.allowed)
        assertEquals(listOf("/dev/sdb"), plan.userDisks)
    }

    @Test
    fun aWorkerHasNoMember() {
        val plan = TalosJson.decodeFromString(NodeResetPlan.serializer(), """{"node":"10.0.0.5","role":"worker","etcdMember":null}""")
        assertNull(plan.etcdMember)
        assertTrue(plan.allowed)
        assertFalse(plan.leavesDeadMember(graceful = false))
    }

    @Test
    fun wipeModesNeedUserDisks() {
        assertEquals(listOf(ResetWipe.SYSTEM), NodeResetPlan().wipeModes)
        assertEquals(ResetWipe.entries, NodeResetPlan(userDisks = listOf("/dev/sdb")).wipeModes)
        assertEquals(listOf("all", "system", "user"), ResetWipe.entries.map { it.wire })
    }

    @Test
    fun aForcedResetOfAMemberLeavesItBehind() {
        val plan = NodeResetPlan(etcdMember = NodeResetMember("a1", true))
        assertTrue(plan.leavesDeadMember(graceful = false))
        assertFalse(plan.leavesDeadMember(graceful = true))
    }
}

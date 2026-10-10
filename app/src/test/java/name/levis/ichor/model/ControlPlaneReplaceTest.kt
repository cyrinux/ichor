package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ControlPlaneReplaceTest {

    private val planJson = """
        {"member":{"id":"a3","hostname":"talos-cp-c","node":"192.0.2.53","found":true,"healthy":false,"reachable":false},
         "quorum":{"members":3,"healthy":2,"afterRemoval":2,"healthyAfter":2,"safe":true},
         "leader":{"id":"a1","node":"192.0.2.51","hostname":"talos-cp-a"},
         "steps":[{"id":"confirmQuorum","state":"done","detail":"2 of 2"},
                  {"id":"removeMember","state":"ready","detail":"sent through talos-cp-a"},
                  {"id":"resetOrPowerOff","state":"skipped","detail":"192.0.2.53 does not answer"},
                  {"id":"bootNewNode","state":"pending","detail":""},
                  {"id":"waitMember","state":"pending","detail":""}],
         "template":{"id":"a1","node":"192.0.2.51","hostname":"talos-cp-a"},
         "future":"ignored"}
    """.trimIndent()

    private fun plan() = TalosJson.decodeFromString(CpReplacePlan.serializer(), planJson)

    @Test
    fun decodesThePlanAndItsSteps() {
        val plan = plan()
        assertEquals("talos-cp-c", plan.member.hostname)
        assertEquals("192.0.2.51", plan.template.node)
        assertEquals(CpStepState.DONE, plan.state(CpStep.CONFIRM_QUORUM))
        assertEquals(CpStepState.READY, plan.state(CpStep.REMOVE_MEMBER))
        assertEquals(CpStepState.SKIPPED, plan.state(CpStep.RESET_OR_POWER_OFF))
        assertEquals(CpStepState.PENDING, plan.state(CpStep.WAIT_MEMBER))
    }

    @Test
    fun unknownOrMissingStepsArePending() {
        val plan = CpReplacePlan(steps = listOf(CpReplaceStep("removeMember", "someday")))
        assertEquals(CpStepState.PENDING, plan.state(CpStep.REMOVE_MEMBER))
        assertEquals(CpStepState.PENDING, plan.state(CpStep.BOOT_NEW_NODE))
    }

    @Test
    fun removalPlanCarriesQuorumAndTheBlocker() {
        val ok = plan().removalPlan
        assertEquals("talos-cp-c", ok.member.confirmToken)
        assertEquals(2, ok.membersAfter)
        assertTrue(ok.allowed)

        val blocked = plan().copy(steps = listOf(CpReplaceStep("removeMember", "blocked", "etcd would lose quorum"))).removalPlan
        assertFalse(blocked.allowed)
        assertEquals(listOf("etcd would lose quorum"), blocked.blockers)
    }

    @Test
    fun waitsPastTheMembersLeftAfterRemoval() {
        assertEquals(2, plan().membersBeforeJoin)
        assertEquals(3, plan().copy(member = plan().member.copy(found = false)).membersBeforeJoin)
    }

    @Test
    fun replaceIsOfferedForFailedVotingMembersOnly() {
        val etcd = EtcdOverview(
            members = listOf(
                EtcdMember("a1", "talos-cp-a", clientUrls = listOf("https://192.0.2.51:2379")),
                EtcdMember("a2", "talos-cp-b", peerUrls = listOf("https://[2001:db8::2]:2380")),
                EtcdMember("a3", "talos-cp-c"),
                EtcdMember("a4", "talos-cp-d", isLearner = true),
            ),
            statuses = listOf(
                EtcdNodeStatus(node = "192.0.2.51", memberId = "a1"),
                EtcdNodeStatus(node = "192.0.2.52", memberId = "a2", errors = listOf("etcdserver: no leader")),
                EtcdNodeStatus(node = "192.0.2.53", error = "unreachable"),
            ),
        )
        assertEquals(listOf("a2", "a3"), etcd.replaceCandidates().map { it.id })
        assertEquals("192.0.2.51", etcd.members[0].address)
        assertEquals("2001:db8::2", etcd.members[1].address)
        assertEquals("", etcd.members[2].address)
    }

    @Test
    fun decodesTheWait() {
        val wait = TalosJson.decodeFromString(
            CpReplaceWait.serializer(),
            """{"joined":false,"members":[{"id":"a1","hostname":"talos-cp-a","healthy":true}],"detail":"a new member is catching up as a learner"}""",
        )
        assertFalse(wait.joined)
        assertEquals(1, wait.members.size)
    }
}

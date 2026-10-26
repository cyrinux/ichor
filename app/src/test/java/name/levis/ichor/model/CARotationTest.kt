package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class CARotationTest {
    private fun progress(phase: String, message: String = "", at: Long = 0) = CARotationProgress(phase = phase, message = message, at = at)

    @Test
    fun theTimelineMarksThePhasesBeforeTheLatestDone() {
        val steps = caRotationTimeline(
            listOf(progress("check", at = 1), progress("accept", "accepting on cp-1", at = 2), progress("accept", "every node applied it", at = 3)),
            finished = false,
            failed = false,
        )
        assertEquals(CARotationPhase.STEPS, steps.map { it.phase })
        assertEquals(listOf(StepStatus.DONE, StepStatus.CURRENT, StepStatus.PENDING, StepStatus.PENDING, StepStatus.PENDING), steps.map { it.status })
        assertEquals("first time, last message", 2L, steps[1].at)
        assertEquals("every node applied it", steps[1].message)
    }

    @Test
    fun aFailedRunFailsItsLatestPhaseAndAFinishedOneIsAllDone() {
        val failed = caRotationTimeline(listOf(progress("check"), progress("accept"), progress("handover")), finished = true, failed = true)
        assertEquals(StepStatus.FAILED, failed[2].status)
        assertEquals(StepStatus.PENDING, failed[3].status)

        val refused = caRotationTimeline(emptyList(), finished = true, failed = true)
        assertEquals("refused before any event: the check failed", StepStatus.FAILED, refused[0].status)

        val done = caRotationTimeline(listOf(progress("check"), progress("done")), finished = true, failed = false)
        assertTrue(done.all { it.status == StepStatus.DONE })
    }

    @Test
    fun thePlanDecodesAsTheCoreWritesIt() {
        val json = """{"context":"admin@prod","endpoints":["10.0.0.1"],"ca":{"subject":"O=talos","notBefore":1,"notAfter":2,"fingerprint":"3f9a1c7e2b8d4650"},
            "nodes":[{"node":"10.0.0.1","hostname":"cp-1","role":"controlplane","version":"v1.14.2","reachable":true,"acceptedCAs":0},
            {"node":"10.0.0.9","hostname":"w-9","role":"unknown","version":"","reachable":false,"acceptedCAs":0,"problem":"does not answer"}],
            "discovered":true,"blockers":["w-9 (10.0.0.9) does not answer"],"warnings":[],"ready":false}"""
        val plan = TalosJson.decodeFromString(CARotationPlan.serializer(), json)
        assertFalse(plan.ready)
        assertEquals("3f9a1c7e2b8d4650", plan.ca?.fingerprint)
        assertEquals(listOf(ServiceHealth.OK, ServiceHealth.CRITICAL), plan.nodes.map { it.health })
    }

    @Test
    fun nodeStatesReadAsHealth() {
        assertEquals(ServiceHealth.OK, caNodeHealth(CARotationNodeState.DONE))
        assertEquals(ServiceHealth.CRITICAL, caNodeHealth(CARotationNodeState.FAILED))
        assertEquals(ServiceHealth.WARNING, caNodeHealth(CARotationNodeState.PATCHING))
        assertEquals(ServiceHealth.IDLE, caNodeHealth(CARotationNodeState.PENDING))
    }
}

package name.levis.ichor.model

import name.levis.ichor.data.TalosJson
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class EtcdFixTest {

    @Test
    fun decodesProgress() {
        val p = TalosJson.decodeFromString(
            EtcdFixProgress.serializer(),
            """{"phase":"defrag","message":"defragmenting cp-2 (1 of 3)","at":5,"step":2,"steps":4,
               "members":[{"node":"10.0.0.2","hostname":"cp-2","state":"running","reclaimedBytes":0}]}""",
        )
        assertEquals(2, p.step)
        assertEquals(4, p.steps)
        assertEquals(EtcdFixMember.STATE_RUNNING, p.members.single().state)
    }

    @Test
    fun nospaceIsDetected() {
        assertTrue(EtcdOverview(alarms = listOf(EtcdAlarm("a1", "NOSPACE"))).hasNospace)
        assertFalse(EtcdOverview(alarms = listOf(EtcdAlarm("a1", "CORRUPT"))).hasNospace)
        assertFalse(EtcdOverview().hasNospace)
    }

    @Test
    fun timelineFollowsTheSteps() {
        val events = listOf(EtcdFixProgress(phase = "snapshot"), EtcdFixProgress(phase = "defrag"))
        assertEquals(
            listOf(StepStatus.DONE, StepStatus.CURRENT, StepStatus.PENDING, StepStatus.PENDING),
            etcdFixTimeline(events, finished = false, failed = false).map { it.second },
        )
        assertEquals(
            listOf(StepStatus.DONE, StepStatus.FAILED, StepStatus.PENDING, StepStatus.PENDING),
            etcdFixTimeline(events, finished = true, failed = true).map { it.second },
        )
        assertTrue(etcdFixTimeline(events, finished = true, failed = false).all { it.second == StepStatus.DONE })
        // Refused before any step: the first one failed.
        assertEquals(StepStatus.FAILED, etcdFixTimeline(emptyList(), finished = true, failed = true).first().second)
    }
}
